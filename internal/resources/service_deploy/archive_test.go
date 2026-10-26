package service_deploy

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDirectoryDeterministic(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "index.js")
	if e := os.WriteFile(p, []byte("hello"), 0644); e != nil {
		t.Fatal(e)
	}
	if e := os.Mkdir(filepath.Join(root, ".git"), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(filepath.Join(root, ".git", "ignored"), []byte("secret"), 0600); e != nil {
		t.Fatal(e)
	}
	first, e := directoryHash(root)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chtimes(p, time.Now(), time.Now().Add(time.Hour)); e != nil {
		t.Fatal(e)
	}
	second, e := directoryHash(root)
	if e != nil || first != second {
		t.Fatalf("unstable hash: %s %s %v", first, second, e)
	}
	var b bytes.Buffer
	if e = writeDirectory(&b, root); e != nil {
		t.Fatal(e)
	}
	gz, e := gzip.NewReader(&b)
	if e != nil {
		t.Fatal(e)
	}
	tr := tar.NewReader(gz)
	h, e := tr.Next()
	if e != nil || h.Name != "index.js" || h.Uid != 0 || !h.ModTime.IsZero() && h.ModTime.Unix() != 0 {
		t.Fatalf("header: %#v %v", h, e)
	}
	if _, e = tr.Next(); e != io.EOF {
		t.Fatalf("unexpected .git entry: %v", e)
	}
	if e = os.WriteFile(p, []byte("changed"), 0644); e != nil {
		t.Fatal(e)
	}
	changed, e := directoryHash(root)
	if e != nil || changed == first {
		t.Fatal("content change was not detected")
	}
}
func TestDirectoryRejectsEscapingSymlink(t *testing.T) {
	root := t.TempDir()
	if e := os.Symlink("../outside", filepath.Join(root, "escape")); e != nil {
		t.Fatal(e)
	}
	if _, e := directoryHash(root); e == nil {
		t.Fatal("accepted escaping symlink")
	}
}
func TestDirectoryPreservesSafeSymlink(t *testing.T) {
	root := t.TempDir()
	if e := os.WriteFile(filepath.Join(root, "file"), []byte("x"), 0755); e != nil {
		t.Fatal(e)
	}
	if e := os.Symlink("file", filepath.Join(root, "link")); e != nil {
		t.Fatal(e)
	}
	if _, e := directoryHash(root); e != nil {
		t.Fatal(e)
	}
}
func TestValidateYAML(t *testing.T) {
	for _, s := range []string{"", "zerops: [", "[]"} {
		if validateYAML(s) == nil {
			t.Fatalf("accepted %q", s)
		}
	}
	if e := validateYAML("zerops:\n  - setup: app\n"); e != nil {
		t.Fatal(e)
	}
}
func TestArchiveHashUsesUploadedBytes(t *testing.T) {
	p := filepath.Join(t.TempDir(), "app.tar.gz")
	data := []byte("the exact uploaded bytes")
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := archiveHash(p)
	want := fmt.Sprintf("%x", sha256.Sum256(data))
	if err != nil || got != want {
		t.Fatalf("got %q, want %q: %v", got, want, err)
	}
	if _, err := archiveHash(filepath.Join(t.TempDir(), "not-an-archive")); err == nil {
		t.Fatal("accepted a non .tar.gz path")
	}
}

func TestDirectorySymlinkChains(t *testing.T) {
	for _, tc := range []struct {
		name, pivot, target string
		bad                 bool
	}{
		{"escape", ".", "pivot/../outside", true},
		{"safe parent", "nested", "pivot/../file", false},
		{"cycle", "escape", "pivot", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, "nested"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "file"), []byte("x"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(tc.pivot, filepath.Join(root, "pivot")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(tc.target, filepath.Join(root, "escape")); err != nil {
				t.Fatal(err)
			}
			var b bytes.Buffer
			err := writeDirectory(&b, root)
			if (err != nil) != tc.bad {
				t.Fatalf("archive error=%v, want bad=%t", err, tc.bad)
			}
			if !tc.bad {
				gz, err := gzip.NewReader(&b)
				if err != nil {
					t.Fatal(err)
				}
				defer gz.Close()
				tr := tar.NewReader(gz)
				h, err := tr.Next()
				if err != nil || h.Name != "escape" || h.Typeflag != tar.TypeSymlink || h.Linkname != tc.target {
					t.Fatalf("link not preserved: %+v, %v", h, err)
				}
			}
		})
	}
}

func TestDirectorySymlinkRootAndTemporaryArchive(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "source")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	want, err := directoryHash(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := directoryHash(link)
	if err != nil || got != want {
		t.Fatalf("symlink root hash=%s want=%s: %v", got, want, err)
	}
	t.Setenv("TMPDIR", link)
	f, err := createSourceArchive(link)
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if filepath.Dir(f.Name()) != parent {
		t.Fatalf("unexpected temporary location: %s", f.Name())
	}
	if err := writeDirectory(f, link); err != nil {
		t.Fatal(err)
	}
	got, err = directoryHash(link)
	if err != nil || got != want {
		t.Fatalf("temporary archive contaminated source: %v", err)
	}
	if f, err := createSourceArchive(string(filepath.Separator)); err == nil {
		f.Close()
		os.Remove(f.Name())
		t.Fatal("accepted source containing every temp location")
	}
}

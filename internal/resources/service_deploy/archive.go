package service_deploy

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// writeDirectory is shared by hashing and materialization: neither timestamps nor
// ownership nor group-write bits can make identical source trees differ.
func writeDirectory(w io.Writer, root string) error {
	root, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("source_dir must be a directory")
	}
	gz := gzip.NewWriter(w)
	gz.Header.ModTime = time.Time{}
	gz.Header.OS = 255
	tw := tar.NewWriter(gz)
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if p == root {
			return nil
		}
		if d.Name() == ".git" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if rel == ".." || strings.HasPrefix(rel, "../") {
			return fmt.Errorf("unsafe archive path")
		}
		st, e := d.Info()
		if e != nil {
			return e
		}
		h := &tar.Header{Name: rel, Mode: 0644, Format: tar.FormatPAX}
		switch {
		case st.Mode().IsRegular():
			h.Typeflag = tar.TypeReg
			h.Size = st.Size()
			if st.Mode()&0111 != 0 {
				h.Mode = 0755
			}
		case st.IsDir():
			h.Typeflag = tar.TypeDir
			h.Mode = 0755
			h.Name += "/"
		case st.Mode()&os.ModeSymlink != 0:
			target, e := os.Readlink(p)
			if e != nil {
				return e
			}
			if e := resolveArchiveLink(root, rel, nil); e != nil {
				return e
			}
			h.Typeflag = tar.TypeSymlink
			h.Mode = 0777
			h.Linkname = filepath.ToSlash(target)
		default:
			return fmt.Errorf("unsupported source file %q", rel)
		}
		if e = tw.WriteHeader(h); e != nil {
			return e
		}
		if st.Mode().IsRegular() {
			f, e := os.Open(p)
			if e != nil {
				return e
			}
			_, e = io.Copy(tw, f)
			ce := f.Close()
			if e != nil {
				return e
			}
			return ce
		}
		return nil
	})
	if err != nil {
		_ = tw.Close()
		_ = gz.Close()
		return err
	}
	if err = tw.Close(); err != nil {
		_ = gz.Close()
		return err
	}
	return gz.Close()
}

// Resolve each component before interpreting the next "..": cleaning a path
// first would miss escapes through links such as pivot -> ".".
func resolveArchiveLink(root, name string, active map[string]bool) error {
	if active == nil {
		active = make(map[string]bool)
	}
	_, err := resolveArchiveComponents(root, nil, strings.Split(name, string(filepath.Separator)), active)
	return err
}

func resolveArchiveComponents(root string, current, parts []string, active map[string]bool) ([]string, error) {
	for _, part := range parts {
		switch part {
		case "", ".":
			continue
		case "..":
			if len(current) == 0 {
				return nil, fmt.Errorf("symlink escapes source directory")
			}
			current = current[:len(current)-1]
			continue
		}
		path := filepath.Join(root, filepath.Join(current...), part)
		info, err := os.Lstat(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		if err == nil && info.Mode()&os.ModeSymlink != 0 {
			if active[path] {
				return nil, fmt.Errorf("symlink cycle at %q", path)
			}
			target, err := os.Readlink(path)
			if err != nil {
				return nil, err
			}
			if filepath.IsAbs(target) {
				return nil, fmt.Errorf("symlink %q escapes source directory", path)
			}
			active[path] = true
			// Do not let recursive append overwrite the caller's components.
			current, err = resolveArchiveComponents(root, append([]string(nil), current...), strings.Split(target, string(filepath.Separator)), active)
			delete(active, path)
			if err != nil {
				return nil, err
			}
		} else {
			current = append(current, part)
		}
	}
	return current, nil
}

func createSourceArchive(source string) (*os.File, error) {
	root, err := filepath.Abs(source)
	if err != nil {
		return nil, err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, err
	}
	// TMPDIR may itself be within source_dir. Walk its canonical parents until
	// finding a writable directory outside the source, never creating inside it.
	dir, err := filepath.Abs(os.TempDir())
	if err != nil {
		return nil, err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	for {
		rel, err := filepath.Rel(root, dir)
		if err != nil {
			return nil, err
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if f, err := os.CreateTemp(dir, "zerops-source-*.tar.gz"); err == nil {
				return f, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("cannot create temporary archive outside source_dir")
		}
		dir = parent
	}
}
func directoryHash(root string) (string, error) {
	h := sha256.New()
	if err := writeDirectory(h, root); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func archiveHash(path string) (string, error) {
	if !strings.HasSuffix(path, ".tar.gz") {
		return "", fmt.Errorf("archive_path must name an existing .tar.gz file")
	}
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	if _, e = io.Copy(h, f); e != nil {
		return "", e
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

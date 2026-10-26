//go:build unix

package client

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

func openLedger(path string) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("production ledger path must be absolute")
	}
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_APPEND|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, errors.New("cannot open production ledger")
	}
	f := os.NewFile(uintptr(fd), path)
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, errors.New("cannot inspect production ledger")
	}
	stat, ok := st.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != uint32(os.Geteuid()) || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
		f.Close()
		return nil, errors.New("production ledger must be a regular, user-owned mode 0600 file")
	}
	if err = syscall.Flock(fd, syscall.LOCK_EX); err != nil {
		f.Close()
		return nil, errors.New("cannot lock production ledger")
	}
	return f, nil
}

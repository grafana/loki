package swfs

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

func CopyFile(from, to string) error {
	r, err := os.Open(from)
	if err != nil {
		return err
	}
	defer r.Close()

	return CopyFileReader(r, to)
}

func CopyFileReader(r io.Reader, to string) error {
	info, err := os.Stat(filepath.Dir(to))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if err := os.MkdirAll(filepath.Dir(to), 0755); err != nil {
				return err
			}
		} else {
			return err
		}
	}

	if info != nil {
		if !info.IsDir() {
			return errors.New("not a directory")
		}
	}

	w, err := os.OpenFile(to, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0755)
	if err != nil {
		return err
	}
	defer w.Close()

	if _, err := io.Copy(w, r); err != nil {
		return err
	}

	return nil
}

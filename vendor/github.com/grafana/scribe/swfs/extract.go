package swfs

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// CopyFS copies the filesystem, returned from "state.GetDirectory(arg)", to the destination (dst).
func CopyFS(dir fs.FS, dst string) error {
	return fs.WalkDir(dir, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if path == "." {
			return nil
		}

		dst := filepath.Join(dst, path)
		info, err := d.Info()
		if err != nil {
			return err
		}

		if d.IsDir() {
			os.Mkdir(dst, info.Mode())
			return nil
		}

		r, err := dir.Open(path)
		if err != nil {
			return err
		}

		w, err := os.Create(dst)
		if err != nil {
			return err
		}

		if _, err := io.Copy(w, r); err != nil {
			return err
		}

		return nil
	})
}

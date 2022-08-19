package swfs

import (
	"crypto/sha256"
	"io"
	"io/fs"
	"os"
)

func HashDirectory(dir string) ([]byte, error) {
	fs := os.DirFS(dir)
	return HashFS(fs)
}

func HashFS(dir fs.FS) ([]byte, error) {
	hashes := map[string][]byte{}

	// Hash each file, add the hash to the map above
	if err := fs.WalkDir(dir, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if path == "." {
			return nil
		}

		if d.IsDir() {
			return nil
		}

		f, err := dir.Open(path)
		if err != nil {
			return err
		}

		defer f.Close()

		b, err := HashFile(f)
		if err != nil {
			return err
		}
		hashes[path] = b
		return nil
	}); err != nil {
		return nil, err
	}

	// For each hashed file, add it to one big hash
	hash := sha256.New()
	for k, b := range hashes {
		hash.Sum([]byte(k))
		hash.Sum(b)
	}

	return hash.Sum(nil), nil
}

func HashFile(r io.Reader) ([]byte, error) {
	hash := sha256.New()

	_, err := io.Copy(hash, r)
	if err != nil {
		return nil, err
	}

	return hash.Sum(nil), nil
}

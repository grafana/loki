package tarfs

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// validate validates the name of the file being extracted to ensure that it does not have illegal directory traversal names before extracting.
// Courtesy of https://snyk.io/research/zip-slip-vulnerability.
func validate(name, dst string) error {
	dstPath := filepath.Join(dst, name)
	if !strings.HasPrefix(dstPath, filepath.Clean(dst)+string(os.PathSeparator)) {
		return fmt.Errorf("%s: illegal file path", name)
	}

	return nil
}

// Untar unarchives and uncompresses a gzipped archive into a path.
func Untar(path string, r io.Reader) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("error creating gzip reader: %w", err)
	}

	defer gr.Close()

	tr := tar.NewReader(gr)

	for {
		header, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				// Then there's no more to read
				return nil
			}
		}
		if header == nil {
			continue
		}

		if err := validate(header.Name, path); err != nil {
			return err
		}

		// use header.Name because we manually set it before and includes the directory name.
		path := filepath.Join(path, header.Name)
		if header.Typeflag == tar.TypeDir {
			if _, err := os.Stat(path); err != nil {
				if err := os.MkdirAll(path, header.FileInfo().Mode()); err != nil {
					return err
				}
			}
		}

		if header.Typeflag == tar.TypeReg {
			dir := filepath.Dir(path)
			if _, err := os.Stat(dir); errors.Is(err, fs.ErrNotExist) {
				if err := os.MkdirAll(dir, 0755); err != nil {
					return err
				}
			}
			f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, header.FileInfo().Mode())
			if err != nil {
				return err
			}

			if _, err := io.Copy(f, tr); err != nil {
				return err
			}

			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			f.Close()
		}
	}
}

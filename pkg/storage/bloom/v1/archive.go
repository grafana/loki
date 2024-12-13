package v1

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/compression"
)

const (
	ExtTar = ".tar"
)

type TarEntry struct {
	Name string
	Size int64
	Body io.ReadSeeker
}

func TarCompress(enc compression.Codec, dst io.Writer, reader BlockReader) error {
	comprPool := compression.GetWriterPool(enc)
	comprWriter := comprPool.GetWriter(dst)
	defer func() {
		comprWriter.Close()
		comprPool.PutWriter(comprWriter)
	}()

	return Tar(comprWriter, reader)
}

func Tar(dst io.Writer, reader BlockReader) error {
	itr, err := reader.TarEntries()
	if err != nil {
		return errors.Wrap(err, "error getting tar entries")
	}

	tarballer := tar.NewWriter(dst)
	defer tarballer.Close()

	for itr.Next() {
		entry := itr.At()
		hdr := &tar.Header{
			Name: entry.Name,
			Mode: 0600,
			Size: entry.Size,
		}

		if err := tarballer.WriteHeader(hdr); err != nil {
			return errors.Wrapf(err, "error writing tar header for file %s", entry.Name)
		}

		if _, err := io.Copy(tarballer, entry.Body); err != nil {
			return errors.Wrapf(err, "error writing file %s to tarball", entry.Name)
		}
	}

	return itr.Err()
}

func UnTarCompress(enc compression.Codec, dst string, r io.Reader) error {
	comprPool := compression.GetReaderPool(enc)
	comprReader, err := comprPool.GetReader(r)
	if err != nil {
		return errors.Wrapf(err, "error getting %s reader", enc.String())
	}
	defer comprPool.PutReader(comprReader)

	return UnTar(dst, comprReader)
}

func UnTar(dst string, r io.Reader) error {
	// Add safety checks for destination
	dst = filepath.Clean(dst)
	if !filepath.IsAbs(dst) {
		return errors.New("destination path must be absolute")
	}
	tarballer := tar.NewReader(r)

	// Track total size to prevent decompression bombs
	var totalSize int64
	const maxSize = 20 << 30 // 20GB limit

	for {
		header, err := tarballer.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Wrap(err, "error reading tarball header")
		}

		// Check for path traversal
		target := filepath.Join(dst, header.Name)
		if !isWithinDir(target, dst) {
			return errors.Errorf("invalid path %q: path traversal attempt", header.Name)
		}

		// Update and check total size
		totalSize += header.Size
		if totalSize > maxSize {
			return errors.New("decompression bomb: extracted content too large")
		}

		// check the file type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0750); err != nil {
					return err
				}
			}

		// if it's a file create it
		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return errors.Wrapf(err, "error creating file %s", target)
			}

			// Use LimitReader to prevent reading more than declared size
			limited := io.LimitReader(tarballer, header.Size)
			written, err := io.Copy(f, limited)
			if err != nil {
				f.Close()
				return errors.Wrapf(err, "error copying contents of file %s", target)
			}

			// Verify the actual bytes written match the header size
			if written != header.Size {
				f.Close()
				return errors.Errorf("size mismatch for %s: header claimed %d bytes but got %d bytes",
					header.Name, header.Size, written)
			}

			if err := f.Close(); err != nil {
				return errors.Wrapf(err, "error closing file %s", target)
			}
		}

	}

	return nil
}

// Helper function to check for path traversal
func isWithinDir(target, dir string) bool {
	targetPath := filepath.Clean(target)
	dirPath := filepath.Clean(dir)

	relative, err := filepath.Rel(dirPath, targetPath)
	if err != nil {
		return false
	}

	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

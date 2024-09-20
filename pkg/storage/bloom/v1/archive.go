package v1

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"

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

func TarCompress(enc compression.Encoding, dst io.Writer, reader BlockReader) error {
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

func UnTarCompress(enc compression.Encoding, dst string, r io.Reader) error {
	comprPool := compression.GetReaderPool(enc)
	comprReader, err := comprPool.GetReader(r)
	if err != nil {
		return errors.Wrapf(err, "error getting %s reader", enc.String())
	}
	defer comprPool.PutReader(comprReader)

	return UnTar(dst, comprReader)
}

func UnTar(dst string, r io.Reader) error {
	tarballer := tar.NewReader(r)

	for {
		header, err := tarballer.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return errors.Wrap(err, "error reading tarball header")
		}

		target := filepath.Join(dst, header.Name)

		// check the file type
		switch header.Typeflag {

		// if its a dir and it doesn't exist create it
		case tar.TypeDir:
			if _, err := os.Stat(target); err != nil {
				if err := os.MkdirAll(target, 0755); err != nil {
					return err
				}
			}

		// if it's a file create it
		case tar.TypeReg:
			f, err := os.OpenFile(target, os.O_CREATE|os.O_RDWR|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return errors.Wrapf(err, "error creating file %s", target)
			}

			// copy over contents
			if _, err := io.Copy(f, tarballer); err != nil {
				return errors.Wrapf(err, "error copying contents of file %s", target)
			}

			// manually close here after each file operation; defering would cause each file close
			// to wait until all operations have completed.
			if err := f.Close(); err != nil {
				return errors.Wrapf(err, "error closing file %s", target)
			}
		}

	}

	return nil
}

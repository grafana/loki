package v1

import (
	"archive/tar"
	"io"
	"os"
	"path/filepath"

	"github.com/grafana/loki/pkg/chunkenc"
	"github.com/pkg/errors"
)

func TarGz(dst io.Writer, src *DirectoryBlockReader) error {
	if err := src.Init(); err != nil {
		return errors.Wrap(err, "error initializing directory block reader")
	}

	gzipper := chunkenc.GetWriterPool(chunkenc.EncGZIP).GetWriter(dst)
	defer gzipper.Close()

	tarballer := tar.NewWriter(gzipper)
	defer tarballer.Close()

	for _, f := range []*os.File{src.index, src.blooms} {
		info, err := f.Stat()
		if err != nil {
			return errors.Wrapf(err, "error stat'ing file %s", f.Name())
		}

		header, err := tar.FileInfoHeader(info, f.Name())
		if err != nil {
			return errors.Wrapf(err, "error creating tar header for file %s", f.Name())
		}

		if err := tarballer.WriteHeader(header); err != nil {
			return errors.Wrapf(err, "error writing tar header for file %s", f.Name())
		}

		if _, err := io.Copy(tarballer, f); err != nil {
			return errors.Wrapf(err, "error writing file %s to tarball", f.Name())
		}

	}
	return nil
}

func UnTarGz(dst string, r io.Reader) error {
	gzipper, err := chunkenc.GetReaderPool(chunkenc.EncGZIP).GetReader(r)
	if err != nil {
		return errors.Wrap(err, "error getting gzip reader")
	}

	tarballer := tar.NewReader(gzipper)

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

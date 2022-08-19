package swfs

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
)

func equalFiles(a fs.FS, b fs.FS, path string) error {
	fileA, err := a.Open(path)
	if err != nil {
		return err
	}
	defer fileA.Close()

	fileAInfo, err := fileA.Stat()
	if err != nil {
		return err
	}

	fileB, err := b.Open(path)
	if err != nil {
		return err
	}
	defer fileB.Close()

	fileBInfo, err := fileB.Stat()
	if err != nil {
		return err
	}

	if fileAInfo.Size() != fileBInfo.Size() {
		return errors.New("file sizes not equal")
	}

	scannerA := bufio.NewScanner(fileA)
	scannerB := bufio.NewScanner(fileB)

	for scannerA.Scan() {
		if !scannerB.Scan() {
			return fmt.Errorf("reached end of file")
		}

		textA := scannerA.Text()
		textB := scannerB.Text()
		if textA != textB {
			return fmt.Errorf("lines not equal: '%s' / '%s'", textA, textB)
		}
	}

	return nil
}

var errorUnequal = "files are not equal"

// Equal ensures that all files and folders in 'a' exist in 'b'.
// This function will return true even if dirB contains more items than 'a', as long as
// all items in 'a' exist in 'b'.
func Equal(a fs.FS, b fs.FS) (bool, error) {
	if err := fs.WalkDir(a, ".", func(path string, d fs.DirEntry, err error) error {
		if path == "." {
			return nil
		}
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if err := equalFiles(a, b, path); err != nil {
			return fmt.Errorf("files not equal '%s' for equality: %w", path, err)
		}

		return nil
	}); err != nil {
		return false, err
	}

	return true, nil
}

//go:build windows
// +build windows

package positions

import (
	"os"
	"path/filepath"

	yaml "gopkg.in/yaml.v2"
)

// writePositionFile is a fall back for Windows because renameio does not support Windows.
// See https://github.com/google/renameio#windows-support
func writePositionFile(filename string, positions map[string]string) error {
	buf, err := yaml.Marshal(File{
		Positions: positions,
	})
	if err != nil {
		return err
	}

	target := filepath.Clean(filename)
	temp := target + "-new"

	err = os.WriteFile(temp, buf, os.FileMode(positionFileMode))
	if err != nil {
		return err
	}

	return os.Rename(temp, target)
}

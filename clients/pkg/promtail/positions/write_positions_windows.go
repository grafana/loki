// +build windows

package positions
import (
	"io/ioutil"
	"os"
	"path/filepath"

	yaml "gopkg.in/yaml.v2"
)

func writePositionFile(filename string, positions map[string]string) error {
	buf, err := yaml.Marshal(File{
		Positions: positions,
	})
	if err != nil {
		return err
	}

	target := filepath.Clean(filename)
	temp := target + "-new"

	err = ioutil.WriteFile(temp, buf, os.FileMode(positionFileMode))
	if err != nil {
		return err
	}

	return os.Rename(temp, target)
}

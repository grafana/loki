package magefile

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/magefile/mage/sh"
)

func rmGlob(glob string) error {
	files, err := filepath.Glob(glob)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := sh.Rm(f); err != nil {
			return err
		}
	}
	return nil
}

func mvGlobToDir(glob, dir string) error {
	files, err := filepath.Glob(glob)
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := os.Rename(f, filepath.Join(dir, f)); err != nil {
			return err
		}
	}
	return nil
}

func revParse(args ...string) string {
	return stdout("git", append([]string{"rev-parse"}, args...)...)
}

func joinMap(mapping map[string]string, pattern string) (out []string) {
	for k, v := range mapping {
		out = append(out, fmt.Sprintf(pattern, k, v))
	}
	return
}

func stdout(cmd string, args ...string) string {
	out, err := sh.Output(cmd, args...)
	if err != nil {
		panic(err)
	}
	return out
}

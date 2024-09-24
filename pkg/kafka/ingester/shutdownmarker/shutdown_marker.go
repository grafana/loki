// SPDX-License-Identifier: AGPL-3.0-only

package shutdownmarker

import (
	"os"
	"path"
	"strings"
	"time"

	"github.com/grafana/dskit/multierror"

	"github.com/grafana/loki/v3/pkg/util/atomicfs"
)

const shutdownMarkerFilename = "shutdown-requested.txt"

// Create writes a marker file on the given path to indicate that a component is
// going to be scaled down in the future. The presence of this file means that a component
// should perform some operations specified by the component itself before being shutdown.
func Create(p string) error {
	return atomicfs.CreateFile(p, strings.NewReader(time.Now().UTC().Format(time.RFC3339)))
}

// Remove removes the shutdown marker file on the given path if it exists.
func Remove(p string) error {
	err := os.Remove(p)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	dir, err := os.OpenFile(path.Dir(p), os.O_RDONLY, 0o777)
	if err != nil {
		return err
	}

	merr := multierror.New()
	merr.Add(dir.Sync())
	merr.Add(dir.Close())
	return merr.Err()
}

// Exists returns true if the shutdown marker file exists on the given path, false otherwise
func Exists(p string) (bool, error) {
	s, err := os.Stat(p)
	if err != nil && os.IsNotExist(err) {
		return false, nil
	}

	if err != nil {
		return false, err
	}

	return s.Mode().IsRegular(), nil
}

// GetPath returns the absolute path of the shutdown marker file
func GetPath(dirPath string) string {
	return path.Join(dirPath, shutdownMarkerFilename)
}

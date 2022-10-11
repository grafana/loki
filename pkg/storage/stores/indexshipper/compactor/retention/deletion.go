package retention

import (
	"context"
	"io/ioutil"
	"os"
	"sort"

	"github.com/go-kit/log/level"

	util_storage "github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

func DeleteChunksBasedOnBlockSize(ctx context.Context, directory string, diskUsage util_storage.DiskStatus, cleanupThreshold int) error {
	if diskUsage.UsedPercent >= float64(cleanupThreshold) {
		if error := purgeOldFiles(diskUsage, directory, cleanupThreshold); error != nil {
			// TODO: handle the error in a better way!
			return error
		}
	}
	return nil
}

func purgeOldFiles(diskUsage util_storage.DiskStatus, directory string, cleanupThreshold int) error {
	files, error := ioutil.ReadDir(directory)
	bytesToDelete := bytesToDelete(diskUsage, cleanupThreshold)
	bytesDeleted := 0.0
	level.Info(util_log.Logger).Log("msg", "bytesToDelete", bytesToDelete)

	if error != nil {
		// TODO: handle the error in a better way!
		return error
	}

	// Sorting by the last modified time
	sort.Slice(files, func(i, j int) bool {
		return files[i].ModTime().Before(files[j].ModTime())
	})

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if bytesToDelete < (bytesDeleted + float64(file.Size())) {
			break
		}

		bytesDeleted = bytesDeleted + float64(file.Size())
		level.Info(util_log.Logger).Log("msg", "block size retention exceded, removing file", "filepath", file.Name())

		if error := os.Remove(directory + "/" + file.Name()); error != nil {
			// TODO: handle the error in a better way!
			return error
		}
	}
	return nil
}

func bytesToDelete(diskUsage util_storage.DiskStatus, cleanupThreshold int) (bytes float64) {
	percentajeToBeDeleted := diskUsage.UsedPercent - float64(cleanupThreshold)

	if percentajeToBeDeleted < 0.0 {
		percentajeToBeDeleted = 0.0
	}

	return ((percentajeToBeDeleted / 100) * float64(diskUsage.All))
}

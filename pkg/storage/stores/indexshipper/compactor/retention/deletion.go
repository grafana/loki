package retention

import (
	"context"
	"os"
	"sort"

	"github.com/go-kit/log/level"

	util_storage "github.com/grafana/loki/pkg/util"
	util_log "github.com/grafana/loki/pkg/util/log"
)

func DeleteChunksBasedOnBlockSize(ctx context.Context, directory string, diskUsage util_storage.DiskStatus, cleanupThreshold int) error {
	if diskUsage.UsedPercent >= float64(cleanupThreshold) {
		if err := purgeOldFiles(diskUsage, directory, cleanupThreshold); err != nil {
			// TODO: handle the error in a better way!
			return err
		}
	}
	return nil
}

func purgeOldFiles(diskUsage util_storage.DiskStatus, directory string, cleanupThreshold int) error {
	files, err := os.ReadDir(directory)
	bytesToDelete := bytesToDelete(diskUsage, cleanupThreshold)
	bytesDeleted := 0.0
	level.Info(util_log.Logger).Log("msg", "bytesToDelete", bytesToDelete)

	if err != nil {
		// TODO: handle the error in a better way!
		return err
	}

	// Sorting by the last modified time
	sort.Slice(files, func(i, j int) bool {
		infoI, _ := files[i].Info()
		infoJ, _ := files[j].Info()
		return infoI.ModTime().Before(infoJ.ModTime())
	})

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		info, _ := file.Info()
		if bytesToDelete < (bytesDeleted + float64(info.Size())) {
			break
		}

		bytesDeleted = bytesDeleted + float64(info.Size())
		level.Info(util_log.Logger).Log("msg", "block size retention exceded, removing file", "filepath", file.Name())

		if err := os.Remove(directory + "/" + file.Name()); err != nil {
			// TODO: handle the error in a better way!
			return err
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

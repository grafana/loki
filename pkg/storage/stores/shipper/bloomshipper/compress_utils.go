package bloomshipper

import (
	"os"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	v1 "github.com/grafana/loki/v3/pkg/storage/bloom/v1"
)

func CompressBloomBlock(ref BlockRef, archivePath, localDst string, logger log.Logger) (Block, error) {
	blockToUpload := Block{}
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return blockToUpload, err
	}

	err = v1.TarGz(archiveFile, v1.NewDirectoryBlockReader(localDst))
	if err != nil {
		level.Error(logger).Log("msg", "creating bloom block archive file", "err", err)
		return blockToUpload, err
	}

	blockToUpload.BlockRef = ref
	blockToUpload.Data = archiveFile

	return blockToUpload, nil
}

package bloomshipper

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
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

func UncompressBloomBlock(block *LazyBlock, workingDirectory string, logger log.Logger) (string, error) {
	workingDirectoryPath := filepath.Join(workingDirectory, block.BlockPath)
	err := os.MkdirAll(workingDirectoryPath, os.ModePerm)
	if err != nil {
		return "", fmt.Errorf("can not create directory to extract the block: %w", err)
	}
	archivePath, err := writeDataToTempFile(workingDirectoryPath, block)
	if err != nil {
		return "", fmt.Errorf("error writing data to temp file: %w", err)
	}
	defer func() {
		os.Remove(archivePath)
		if err != nil {
			level.Error(logger).Log("msg", "removing archive file", "err", err, "file", archivePath)
		}
	}()
	err = extractArchive(archivePath, workingDirectoryPath)
	if err != nil {
		return "", fmt.Errorf("error extracting archive: %w", err)
	}
	return workingDirectoryPath, nil
}

func writeDataToTempFile(workingDirectoryPath string, block *LazyBlock) (string, error) {
	defer block.Data.Close()
	archivePath := filepath.Join(workingDirectoryPath, block.BlockPath[strings.LastIndex(block.BlockPath, "/")+1:])

	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return "", fmt.Errorf("error creating empty file to store the archiver: %w", err)
	}
	defer archiveFile.Close()
	_, err = io.Copy(archiveFile, block.Data)
	if err != nil {
		return "", fmt.Errorf("error writing data to archive file: %w", err)
	}
	return archivePath, nil
}

func extractArchive(archivePath string, workingDirectoryPath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("error opening archive file %s: %w", file.Name(), err)
	}
	return v1.UnTarGz(workingDirectoryPath, file)
}

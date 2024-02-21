package bloomshipper

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/google/uuid"

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

func writeDataToTempFile(workingDirectoryPath string, data io.ReadCloser) (string, error) {
	defer data.Close()
	archivePath := filepath.Join(workingDirectoryPath, uuid.New().String())

	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return "", fmt.Errorf("error creating empty file to store the archiver: %w", err)
	}
	defer archiveFile.Close()
	_, err = io.Copy(archiveFile, data)
	if err != nil {
		return "", fmt.Errorf("error writing data to archive file: %w", err)
	}
	return archivePath, nil
}

func extractArchive(archivePath string, workingDirectoryPath string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("error opening archive file %s: %w", archivePath, err)
	}
	return v1.UnTarGz(workingDirectoryPath, file)
}

func extractBlock(data io.ReadCloser, blockDir string, logger log.Logger) error {
	err := os.MkdirAll(blockDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("can not create directory to extract the block: %w", err)
	}
	archivePath, err := writeDataToTempFile(blockDir, data)
	if err != nil {
		return fmt.Errorf("error writing data to temp file: %w", err)
	}
	defer func() {
		err = os.Remove(archivePath)
		if err != nil {
			level.Error(logger).Log("msg", "error removing temp archive file", "err", err)
		}
	}()
	err = extractArchive(archivePath, blockDir)
	if err != nil {
		return fmt.Errorf("error extracting archive: %w", err)
	}
	return nil
}

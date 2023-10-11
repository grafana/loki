package bloomshipper

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/exp/slices"

	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/bloomshipperconfig"
)

type Shipper struct {
	client Client
	config bloomshipperconfig.Config
}

func NewShipper(client Client, config bloomshipperconfig.Config) (*Shipper, error) {
	return &Shipper{
		client: client,
		config: config,
	}, nil
}

func (s *Shipper) ForEachBlock(
	ctx context.Context,
	tenantID string,
	startTimestamp, endTimestamp int64,
	minFingerprint, maxFingerprint uint64,
	callback func(bq *v1.BlockQuerier) error) error {

	blockRefs, err := s.getActiveBlockRefs(ctx, tenantID, startTimestamp, endTimestamp, minFingerprint, maxFingerprint)
	if err != nil {
		return fmt.Errorf("error fetching active block references : %w", err)
	}

	blocksChannel, errorsChannel := s.client.GetBlocks(ctx, blockRefs)
	for {
		select {
		case block, ok := <-blocksChannel:
			if !ok {
				return nil
			}
			directory, err := s.extractBlock(&block, time.Now().UTC())
			if err != nil {
				return fmt.Errorf("error unarchiving block %s err: %w", block.BlockPath, err)
			}
			blockQuerier := s.createBlockQuerier(directory)
			err = callback(blockQuerier)
			if err != nil {
				return fmt.Errorf("error running callback function for block %s err: %w", block.BlockPath, err)
			}
		case err := <-errorsChannel:
			if err != nil {
				return fmt.Errorf("error downloading blocks : %w", err)
			}
		}
	}
}

func (s *Shipper) getActiveBlockRefs(
	ctx context.Context,
	tenantID string,
	startTimestamp, endTimestamp int64,
	minFingerprint, maxFingerprint uint64) ([]BlockRef, error) {
	metas, err := s.client.GetMetas(ctx, MetaSearchParams{
		TenantID:       tenantID,
		MinFingerprint: minFingerprint,
		MaxFingerprint: maxFingerprint,
		StartTimestamp: startTimestamp,
		EndTimestamp:   endTimestamp,
	})
	if err != nil {
		return []BlockRef{}, fmt.Errorf("error fetching meta.json files: %w", err)
	}
	activeBlocks := s.findBlocks(metas, minFingerprint, maxFingerprint, startTimestamp, endTimestamp)
	slices.SortStableFunc(activeBlocks, func(a, b BlockRef) int {
		if a.MinFingerprint < b.MinFingerprint {
			return -1
		}
		if a.MinFingerprint > b.MinFingerprint {
			return 1
		}

		return 0
	})
	return activeBlocks, nil
}

func (s *Shipper) findBlocks(
	metas []Meta,
	minFingerprint uint64,
	maxFingerprint uint64,
	startTimestamp int64,
	endTimestamp int64,
) []BlockRef {
	outdatedBlocks := make(map[string]interface{})
	for _, meta := range metas {
		for _, tombstone := range meta.Tombstones {
			outdatedBlocks[tombstone.BlockPath] = nil
		}
	}
	blocksSet := make(map[string]BlockRef)
	for _, meta := range metas {
		for _, block := range meta.Blocks {
			if _, contains := outdatedBlocks[block.BlockPath]; contains {
				continue
			}
			if isOutsideRange(&block, minFingerprint, maxFingerprint, startTimestamp, endTimestamp) {
				continue
			}
			blocksSet[block.BlockPath] = block
		}
	}
	blockRefs := make([]BlockRef, 0, len(blocksSet))
	for _, ref := range blocksSet {
		blockRefs = append(blockRefs, ref)
	}
	return blockRefs
}

func isOutsideRange(
	b *BlockRef,
	minFingerprint uint64,
	maxFingerprint uint64,
	startTimestamp int64,
	endTimestamp int64,
) bool {
	return b.MaxFingerprint < minFingerprint || b.MinFingerprint > maxFingerprint ||
		b.EndTimestamp < startTimestamp || b.StartTimestamp > endTimestamp
}

// unzip the bytes into directory and returns absolute path to this directory.
func (s *Shipper) extractBlock(block *Block, ts time.Time) (string, error) {
	workingDirectoryPath := filepath.Join(s.config.WorkingDirectory, block.BlockPath, strconv.FormatInt(ts.UnixMilli(), 10))
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
		// todo log err
	}()
	err = extractArchive(archivePath, workingDirectoryPath)
	if err != nil {
		return "", fmt.Errorf("error extracting archive: %w", err)
	}
	return workingDirectoryPath, nil
}

func (s *Shipper) createBlockQuerier(directory string) *v1.BlockQuerier {
	reader := v1.NewDirectoryBlockReader(directory)
	block := v1.NewBlock(reader)
	return v1.NewBlockQuerier(block)
}

func writeDataToTempFile(workingDirectoryPath string, block *Block) (string, error) {
	defer block.Data.Close()
	archivePath := filepath.Join(workingDirectoryPath, block.BlockPath[strings.LastIndex(block.BlockPath, delimiter)+1:])

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
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("error opening archive: %w", err)
	}
	defer reader.Close()
	for _, file := range reader.File {
		err := extractInnerFile(file, workingDirectoryPath)
		if err != nil {
			return fmt.Errorf("error extracting %s file from archive: %w", file.Name, err)
		}
	}
	return nil
}

func extractInnerFile(file *zip.File, workingDirectoryPath string) error {
	innerFile, err := file.Open()
	if err != nil {
		return fmt.Errorf("error opening file: %w", err)
	}
	defer innerFile.Close()
	extractedInnerFile, err := os.Create(filepath.Join(workingDirectoryPath, file.Name))
	if err != nil {
		return fmt.Errorf("error creating empty file: %w", err)
	}
	defer extractedInnerFile.Close()
	_, err = io.Copy(extractedInnerFile, innerFile)
	if err != nil {
		return fmt.Errorf("error writing data: %w", err)
	}
	return nil
}

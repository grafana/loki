package bloomshipper

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/exp/slices"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/config"
)

type Shipper struct {
	client Client
	config config.Config
	logger log.Logger
}

func NewShipper(client Client, config config.Config, logger log.Logger) (*Shipper, error) {
	return &Shipper{
		client: client,
		config: config,
		logger: log.With(logger, "component", "bloom-shipper"),
	}, nil
}

func (s *Shipper) ForEachBlock(
	ctx context.Context,
	tenantID string,
	from, through time.Time,
	fingerprints []uint64,
	callback ForEachBlockCallback) error {

	level.Debug(s.logger).Log("msg", "ForEachBlock", "tenant", tenantID, "from", from, "through", through, "fingerprints", fingerprints)

	blockRefs, err := s.getActiveBlockRefs(ctx, tenantID, from.UnixNano(), through.UnixNano(), fingerprints)
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

func (s *Shipper) Stop() {
	s.client.Stop()
}

// getFromThrough returns the first and list item of a fingerprint slice
// It assumes an ascending sorted list of fingerprints.
func getFromThrough(fingerprints []uint64) (uint64, uint64) {
	if len(fingerprints) == 0 {
		return 0, 0
	}
	return fingerprints[0], fingerprints[len(fingerprints)-1]
}

func (s *Shipper) getActiveBlockRefs(
	ctx context.Context,
	tenantID string,
	from, through int64,
	fingerprints []uint64) ([]BlockRef, error) {

	minFingerprint, maxFingerprint := getFromThrough(fingerprints)
	metas, err := s.client.GetMetas(ctx, MetaSearchParams{
		TenantID:       tenantID,
		MinFingerprint: minFingerprint,
		MaxFingerprint: maxFingerprint,
		StartTimestamp: from,
		EndTimestamp:   through,
	})
	if err != nil {
		return []BlockRef{}, fmt.Errorf("error fetching meta.json files: %w", err)
	}
	activeBlocks := s.findBlocks(metas, from, through, fingerprints)
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

func (s *Shipper) findBlocks(metas []Meta, startTimestamp, endTimestamp int64, fingerprints []uint64) []BlockRef {
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
			if isOutsideRange(&block, startTimestamp, endTimestamp, fingerprints) {
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

// getPosition returns the index of element v in slice s when v >= elem
// TODO(chaudum): Use binary search to find index instead of iteration.
func getPosition[S ~[]E, E cmp.Ordered](s S, v E) int {
	for i := range s {
		if v > s[i] {
			continue
		}
		return i
	}
	return len(s)
}

func isOutsideRange(b *BlockRef, startTimestamp, endTimestamp int64, fingerprints []uint64) bool {
	// First, check time range
	if b.EndTimestamp < startTimestamp || b.StartTimestamp > endTimestamp {
		return true
	}

	// Then, check if outside of min/max of fingerprint slice
	minFp, maxFp := getFromThrough(fingerprints)
	if b.MaxFingerprint < minFp || b.MinFingerprint > maxFp {
		return true
	}

	// Check if the block range is inside a "gap" in the fingerprint slice
	// e.g.
	// fingerprints = [1, 2,          6, 7, 8]
	// block =              [3, 4, 5]
	idx := getPosition[[]uint64](fingerprints, b.MinFingerprint)
	return b.MaxFingerprint < fingerprints[idx]
}

// extract the files into directory and returns absolute path to this directory.
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
	file, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("error opening archive file %s: %w", file.Name(), err)
	}
	return v1.UnTarGz(workingDirectoryPath, file)
}

package audit

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	progressbar "github.com/schollz/progressbar/v3"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/compactor"
	"github.com/grafana/loki/v3/pkg/compactor/retention"
	"github.com/grafana/loki/v3/pkg/storage"
	loki_storage "github.com/grafana/loki/v3/pkg/storage"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	indexshipper_storage "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	shipperutil "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	TsFormat = time.RFC3339Nano
)

func Run(ctx context.Context, cloudIndexPath, table string, cfg Config, logger log.Logger) (int, int, error) {
	level.Info(logger).Log("msg", "auditing index", "index", cloudIndexPath, "table", table, "tenant", cfg.Tenant, "working_dir", cfg.WorkingDir)

	objClient, err := GetObjectClient(cfg)
	if err != nil {
		return 0, 0, err
	}

	localFile, err := DownloadIndexFile(ctx, cfg, cloudIndexPath, objClient, logger)
	if err != nil {
		return 0, 0, err
	}

	compactedIdx, err := ParseCompactexIndex(ctx, localFile, table, cfg)
	if err != nil {
		return 0, 0, err
	}
	defer compactedIdx.Cleanup()

	return ValidateCompactedIndex(ctx, objClient, compactedIdx, cfg.Concurrency, logger)
}

func GetObjectClient(cfg Config) (client.ObjectClient, error) {
	periodCfg := cfg.SchemaConfig.Configs[len(cfg.SchemaConfig.Configs)-1] // only check the last period.

	objClient, err := loki_storage.NewObjectClient(periodCfg.ObjectType, cfg.StorageConfig, storage.NewClientMetrics())
	if err != nil {
		return nil, fmt.Errorf("couldn't create object client: %w", err)
	}

	return objClient, nil
}

func DownloadIndexFile(ctx context.Context, cfg Config, cloudIndexPath string, objClient client.ObjectClient, logger log.Logger) (string, error) {
	splitPath := strings.Split(cloudIndexPath, "/")
	localFileName := splitPath[len(splitPath)-1]
	decompress := indexshipper_storage.IsCompressedFile(cloudIndexPath)
	if decompress {
		// get rid of the last extension, which is .gz
		localFileName = strings.TrimSuffix(localFileName, path.Ext(localFileName))
	}
	localFilePath := path.Join(cfg.WorkingDir, localFileName)
	if err := shipperutil.DownloadFileFromStorage(localFilePath, decompress, false, logger, func() (io.ReadCloser, error) {
		r, _, err := objClient.GetObject(ctx, cloudIndexPath)
		return r, err
	}); err != nil {
		return "", fmt.Errorf("couldn't download file %q from storage: %w", cloudIndexPath, err)
	}

	level.Info(logger).Log("msg", "file successfully downloaded from storage", "path", cloudIndexPath)
	return localFileName, nil
}

func ParseCompactexIndex(ctx context.Context, localFilePath, table string, cfg Config) (compactor.CompactedIndex, error) {
	periodCfg := cfg.SchemaConfig.Configs[len(cfg.SchemaConfig.Configs)-1] // only check the last period.
	idxCompactor := tsdb.NewIndexCompactor()
	compactedIdx, err := idxCompactor.OpenCompactedIndexFile(ctx, localFilePath, table, cfg.Tenant, cfg.WorkingDir, periodCfg, util_log.Logger)
	if err != nil {
		return nil, fmt.Errorf("couldn't open compacted index file %q: %w", localFilePath, err)
	}
	return compactedIdx, nil
}

func ValidateCompactedIndex(ctx context.Context, objClient client.ObjectClient, compactedIdx compactor.CompactedIndex, parallelism int, logger log.Logger) (int, int, error) {
	var missingChunks, foundChunks atomic.Int32
	foundChunks.Store(0)
	missingChunks.Store(0)
	bar := progressbar.NewOptions(-1,
		progressbar.OptionShowCount(),
		progressbar.OptionSetDescription("Chunks validated"),
	)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(parallelism)
	compactedIdx.ForEachChunk(ctx, func(ce retention.ChunkEntry) (deleteChunk bool, err error) { //nolint:errcheck
		bar.Add(1) // nolint:errcheck
		g.Go(func() error {
			exists, err := CheckChunkExistance(string(ce.ChunkID), objClient)
			if err != nil || !exists {
				missingChunks.Add(1)
				logger.Log("msg", "chunk is missing", "err", err, "chunk_id", string(ce.ChunkID))
				return nil
			}
			foundChunks.Add(1)
			return nil
		})

		return false, nil
	})
	g.Wait() // nolint:errcheck

	return int(foundChunks.Load()), int(missingChunks.Load()), nil
}

func CheckChunkExistance(key string, objClient client.ObjectClient) (bool, error) {
	exists, err := objClient.ObjectExists(context.Background(), key)
	if err != nil {
		return false, err
	}

	return exists, nil
}

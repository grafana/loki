package audit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	TsFormat = time.RFC3339Nano
)

type Audit struct {
	workingDir string

	tenant string

	logger log.Logger

	period string
	fp     string

	parallelism int
}

func (a *Audit) validate() error {
	if a.parallelism < 1 {
		return fmt.Errorf("invalid parallelism %d", a.parallelism)
	}

	if a.logger == nil {
		return fmt.Errorf("logger should be set")
	}

	if a.tenant == "" {
		return fmt.Errorf("tenant should be set")
	}

	if a.workingDir == "" {
		return fmt.Errorf("please set the workingdir")
	}

	return nil
}

func Run(ctx context.Context, path, table, tenant, workingDir string) error {
	gzipExtension := ".gz"
	if decompress := storage.IsCompressedFile(path); decompress {
		path = strings.Trim(path, gzipExtension)
	}

	idxCompactor := tsdb.NewIndexCompactor()
	compactedIdx, err := idxCompactor.OpenCompactedIndexFile(ctx, path, table, tenant, workingDir, config.PeriodConfig{}, util_log.Logger)
	if err != nil {
		return err
	}
	defer compactedIdx.Cleanup()

	return nil
}

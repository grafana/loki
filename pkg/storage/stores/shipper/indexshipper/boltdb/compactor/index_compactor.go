package compactor

import (
	"context"

	"github.com/go-kit/log"

	"github.com/grafana/loki/v3/pkg/compactor"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

const (
	batchSize = 1000
)

type indexEntry struct {
	k, v []byte
}

type indexCompactor struct{}

func NewIndexCompactor() compactor.IndexCompactor {
	return indexCompactor{}
}

func (i indexCompactor) NewTableCompactor(ctx context.Context, commonIndexSet compactor.IndexSet, existingUserIndexSet map[string]compactor.IndexSet, userIndexSetFactoryFunc compactor.MakeEmptyUserIndexSetFunc, periodConfig config.PeriodConfig) compactor.TableCompactor {
	return newTableCompactor(ctx, commonIndexSet, existingUserIndexSet, userIndexSetFactoryFunc, periodConfig)
}

func (i indexCompactor) OpenCompactedIndexFile(_ context.Context, path, tableName, _, workingDir string, periodConfig config.PeriodConfig, logger log.Logger) (compactor.CompactedIndex, error) {
	boltdb, err := openBoltdbFileWithNoSync(path)
	if err != nil {
		return nil, err
	}

	return newCompactedIndex(boltdb, tableName, workingDir, periodConfig, logger), nil
}

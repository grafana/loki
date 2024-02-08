package bloomcompactor

import (
	v1 "github.com/grafana/loki/pkg/storage/bloom/v1"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb"
)

const (
	BloomPrefix = "bloom"
	MetasPrefix = "metas"
)

type TSDBStore interface {
	ResolveTSDBs() ([]*tsdb.SingleTenantTSDBIdentifier, error)
	LoadTSDB(id tsdb.Identifier, bounds v1.FingerprintBounds) (v1.CloseableIterator[*v1.Series], error)
}

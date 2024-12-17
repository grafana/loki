package local

import (
	"io"
	"os"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
)

type fixture struct {
	name    string
	dirname string
}

func (f *fixture) Name() string {
	return f.name
}

func (f *fixture) Clients() (
	indexClient index.Client, chunkClient client.Client, tableClient index.TableClient,
	schemaConfig config.SchemaConfig, closer io.Closer, err error,
) {
	f.dirname, err = os.MkdirTemp(os.TempDir(), "boltdb")
	if err != nil {
		return
	}

	indexClient, err = NewBoltDBIndexClient(BoltDBConfig{
		Directory: f.dirname,
	})
	if err != nil {
		return
	}

	oClient, err := NewFSObjectClient(FSConfig{Directory: f.dirname})
	if err != nil {
		return
	}

	chunkClient = client.NewClient(oClient, client.FSEncoder, config.SchemaConfig{})

	tableClient, err = NewTableClient(f.dirname)
	if err != nil {
		return
	}

	schemaConfig = config.SchemaConfig{
		Configs: []config.PeriodConfig{{
			IndexType: "boltdb",
			From:      config.DayTime{Time: model.Now()},
			ChunkTables: config.PeriodicTableConfig{
				Prefix: "chunks",
				Period: 10 * time.Minute,
			},
			IndexTables: config.IndexPeriodicTableConfig{
				PeriodicTableConfig: config.PeriodicTableConfig{
					Prefix: "index",
					Period: 10 * time.Minute,
				}},
		}},
	}

	closer = testutils.CloserFunc(func() error {
		return os.RemoveAll(f.dirname)
	})

	return
}

// Fixtures for unit testing GCP storage.
var Fixtures = []testutils.Fixture{
	&fixture{
		name: "boltdb",
	},
}

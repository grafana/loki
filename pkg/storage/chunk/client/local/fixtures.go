package local

import (
	"io"
	"io/ioutil"
	"os"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/client/objectclient"
	"github.com/grafana/loki/pkg/storage/chunk/config"
	"github.com/grafana/loki/pkg/storage/chunk/index"
	"github.com/grafana/loki/pkg/storage/chunk/testutils"
)

type fixture struct {
	name    string
	dirname string
}

func (f *fixture) Name() string {
	return f.name
}

func (f *fixture) Clients() (
	indexClient index.IndexClient, chunkClient chunk.Client, tableClient index.TableClient,
	schemaConfig config.SchemaConfig, closer io.Closer, err error,
) {
	f.dirname, err = ioutil.TempDir(os.TempDir(), "boltdb")
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

	chunkClient = objectclient.NewClient(oClient, objectclient.FSEncoder, config.SchemaConfig{})

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
			IndexTables: config.PeriodicTableConfig{
				Prefix: "index",
				Period: 10 * time.Minute,
			},
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

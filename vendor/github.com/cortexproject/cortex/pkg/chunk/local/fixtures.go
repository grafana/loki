package local

import (
	"io"
	"io/ioutil"
	"os"
	"time"

	"github.com/prometheus/common/model"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/objectclient"
	"github.com/cortexproject/cortex/pkg/chunk/testutils"
)

type fixture struct {
	name    string
	dirname string
}

func (f *fixture) Name() string {
	return f.name
}

func (f *fixture) Clients() (
	indexClient chunk.IndexClient, chunkClient chunk.Client, tableClient chunk.TableClient,
	schemaConfig chunk.SchemaConfig, closer io.Closer, err error,
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

	chunkClient = objectclient.NewClient(oClient, objectclient.Base64Encoder)

	tableClient, err = NewTableClient(f.dirname)
	if err != nil {
		return
	}

	schemaConfig = chunk.SchemaConfig{
		Configs: []chunk.PeriodConfig{{
			IndexType: "boltdb",
			From:      chunk.DayTime{Time: model.Now()},
			ChunkTables: chunk.PeriodicTableConfig{
				Prefix: "chunks",
				Period: 10 * time.Minute,
			},
			IndexTables: chunk.PeriodicTableConfig{
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

package local

import (
	"io"
	"os"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
)

type indexFixture struct {
	name    string
	dirname string
}

func (f indexFixture) Name() string {
	return f.name
}

func (f indexFixture) Client() (
	indexClient index.Client,
	closer io.Closer, err error,
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

	closer = testutils.CloserFunc(func() error {
		return os.RemoveAll(f.dirname)
	})

	return
}

type chunkFixture struct {
	name    string
	dirname string
}

func (f chunkFixture) Name() string {
	return f.name
}

func (f chunkFixture) Client() (
	chunkClient client.Client,
	closer io.Closer, err error,
) {
	f.dirname, err = os.MkdirTemp(os.TempDir(), "boltdb")
	if err != nil {
		return
	}

	oClient, err := NewFSObjectClient(FSConfig{Directory: f.dirname})
	if err != nil {
		return
	}

	chunkClient = client.NewClient(oClient, client.FSEncoder, config.SchemaConfig{})

	closer = testutils.CloserFunc(func() error {
		return os.RemoveAll(f.dirname)
	})

	return
}

var IndexFixture = indexFixture{name: "boltdb"}
var ChunkFixture = chunkFixture{name: "fs"}

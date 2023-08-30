package gcp

import (
	"context"
	"io"

	"cloud.google.com/go/storage"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"google.golang.org/api/option"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/pkg/storage/config"
)

type fixture struct {
	name string
}

func (f fixture) Name() string {
	return f.name
}

func (f fixture) Client() (cClient client.Client, closer io.Closer, err error) {
	gcssrv := fakestorage.NewServer(nil)
	gcssrv.CreateBucket("chunks")

	var c *GCSObjectClient
	c, err = newGCSObjectClient(context.Background(), GCSConfig{
		BucketName: "chunks",
		Insecure:   true,
	}, hedging.Config{}, func(ctx context.Context, opts ...option.ClientOption) (*storage.Client, error) {
		return gcssrv.Client(), nil
	})
	if err != nil {
		return
	}

	cClient = client.NewClient(c, nil, config.SchemaConfig{})
	closer = testutils.CloserFunc(func() error {
		gcssrv.Stop()
		return nil
	})

	return
}

// Fixtures for unit testing GCS storage.
var ChunkFixture = func() testutils.ChunkFixture {
	return &fixture{name: "gcs"}
}()

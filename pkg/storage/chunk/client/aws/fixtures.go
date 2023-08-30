package aws

import (
	"io"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/pkg/storage/config"
)

type fixture struct {
	name string
}

func (f fixture) Name() string {
	return f.name
}

func (f fixture) Client() (client.Client, io.Closer, error) {
	mock := newMockS3()
	object := client.NewClient(&S3ObjectClient{S3: mock, hedgedS3: mock}, nil, config.SchemaConfig{})

	return object, testutils.CloserFunc(func() error {
		object.Stop()
		return nil
	}), nil
}

// Fixtures for testing the various configuration of AWS storage.
var ChunkFixture = fixture{
	name: "S3 chunks",
}

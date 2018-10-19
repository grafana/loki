package gcp

import (
	"context"
	"time"

	"cloud.google.com/go/bigtable"
	"cloud.google.com/go/bigtable/bttest"
	"github.com/prometheus/common/model"
	"google.golang.org/api/option"
	"google.golang.org/grpc"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/testutils"
)

const (
	proj, instance = "proj", "instance"
)

type fixture struct {
	srv  *bttest.Server
	name string

	columnKeyClient bool
}

func (f *fixture) Name() string {
	return f.name
}

func (f *fixture) Clients() (
	sClient chunk.StorageClient, tClient chunk.TableClient,
	schemaConfig chunk.SchemaConfig, err error,
) {
	f.srv, err = bttest.NewServer("localhost:0")
	if err != nil {
		return
	}

	conn, err := grpc.Dial(f.srv.Addr, grpc.WithInsecure())
	if err != nil {
		return
	}

	ctx := context.Background()
	adminClient, err := bigtable.NewAdminClient(ctx, proj, instance, option.WithGRPCConn(conn))
	if err != nil {
		return
	}

	client, err := bigtable.NewClient(ctx, proj, instance, option.WithGRPCConn(conn))
	if err != nil {
		return
	}

	schemaConfig = chunk.SchemaConfig{
		Configs: []chunk.PeriodConfig{{
			Store: "gcp",
			From:  model.Now(),
			ChunkTables: chunk.PeriodicTableConfig{
				Prefix: "chunks",
				Period: 10 * time.Minute,
			},
		}},
	}
	tClient = &tableClient{
		client: adminClient,
	}

	if f.columnKeyClient {
		sClient = newStorageClientColumnKey(Config{}, client, schemaConfig)
	} else {
		sClient = newStorageClientV1(Config{}, client, schemaConfig)
	}

	return
}

func (f *fixture) Teardown() error {
	f.srv.Close()
	return nil
}

// Fixtures for unit testing GCP storage.
var Fixtures = []testutils.Fixture{
	&fixture{
		name:            "GCP-ColumnKey",
		columnKeyClient: true,
	},
	&fixture{
		name: "GCPv1",
	},
}

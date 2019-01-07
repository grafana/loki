package gcp

import (
	"context"
	"time"

	"cloud.google.com/go/bigtable"
	"cloud.google.com/go/bigtable/bttest"
	"github.com/fsouza/fake-gcs-server/fakestorage"
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
	btsrv  *bttest.Server
	gcssrv *fakestorage.Server

	name string

	gcsObjectClient bool
	columnKeyClient bool
}

func (f *fixture) Name() string {
	return f.name
}

func (f *fixture) Clients() (
	iClient chunk.IndexClient, cClient chunk.ObjectClient, tClient chunk.TableClient,
	schemaConfig chunk.SchemaConfig, err error,
) {
	f.btsrv, err = bttest.NewServer("localhost:0")
	if err != nil {
		return
	}

	f.gcssrv = fakestorage.NewServer(nil)
	f.gcssrv.CreateBucket("chunks")

	conn, err := grpc.Dial(f.btsrv.Addr, grpc.WithInsecure())
	if err != nil {
		return
	}

	ctx := context.Background()
	adminClient, err := bigtable.NewAdminClient(ctx, proj, instance, option.WithGRPCConn(conn))
	if err != nil {
		return
	}

	schemaConfig = chunk.SchemaConfig{
		Configs: []chunk.PeriodConfig{{
			IndexType: "gcp",
			From:      model.Now(),
			ChunkTables: chunk.PeriodicTableConfig{
				Prefix: "chunks",
				Period: 10 * time.Minute,
			},
		}},
	}
	tClient = &tableClient{
		client: adminClient,
	}

	client, err := bigtable.NewClient(ctx, proj, instance, option.WithGRPCConn(conn))
	if err != nil {
		return
	}

	if f.columnKeyClient {
		iClient = newStorageClientColumnKey(Config{}, schemaConfig, client)
	} else {
		iClient = newStorageClientV1(Config{}, schemaConfig, client)
	}

	if f.gcsObjectClient {
		cClient = newGCSObjectClient(GCSConfig{
			BucketName: "chunks",
		}, schemaConfig, f.gcssrv.Client())
	} else {
		cClient = newBigtableObjectClient(Config{}, schemaConfig, client)
	}

	return
}

func (f *fixture) Teardown() error {
	f.btsrv.Close()
	f.gcssrv.Stop()
	return nil
}

// Fixtures for unit testing GCP storage.
var Fixtures = []testutils.Fixture{
	&fixture{
		name: "bigtable",
	},
	&fixture{
		name:            "bigtable-columnkey",
		columnKeyClient: true,
	},
	&fixture{
		name:            "bigtable-gcs",
		gcsObjectClient: true,
	},
	&fixture{
		name:            "bigtable-columnkey-gcs",
		gcsObjectClient: true,
		columnKeyClient: true,
	},
}

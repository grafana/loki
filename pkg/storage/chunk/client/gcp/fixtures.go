package gcp

import (
	"context"
	"fmt"
	"io"

	"cloud.google.com/go/bigtable"
	"cloud.google.com/go/bigtable/bttest"
	"cloud.google.com/go/storage"
	"github.com/fsouza/fake-gcs-server/fakestorage"
	"google.golang.org/api/option"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
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
	hashPrefix      bool
}

func (f *fixture) Name() string {
	return f.name
}

func (f *fixture) Clients() (
	iClient index.Client, cClient client.Client, tClient index.TableClient,
	schemaConfig config.SchemaConfig, closer io.Closer, err error,
) {
	f.btsrv, err = bttest.NewServer("localhost:0")
	if err != nil {
		return
	}

	f.gcssrv = fakestorage.NewServer(nil)

	opts := fakestorage.CreateBucketOpts{
		Name: "chunks",
	}
	f.gcssrv.CreateBucketWithOpts(opts)

	conn, err := grpc.NewClient(f.btsrv.Addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return
	}

	ctx := context.Background()
	adminClient, err := bigtable.NewAdminClient(ctx, proj, instance, option.WithGRPCConn(conn))
	if err != nil {
		return
	}

	schemaConfig = testutils.DefaultSchemaConfig("gcp-columnkey")
	tClient = &tableClient{
		client: adminClient,
	}

	bigTableClientConfig := bigtable.ClientConfig{
		MetricsProvider: bigtable.NoopMetricsProvider{},
	}
	c, err := bigtable.NewClientWithConfig(ctx, proj, instance, bigTableClientConfig, option.WithGRPCConn(conn))
	if err != nil {
		return
	}

	cfg := Config{
		DistributeKeys: f.hashPrefix,
	}
	if f.columnKeyClient {
		iClient = newStorageClientColumnKey(cfg, schemaConfig, c)
	} else {
		iClient = newStorageClientV1(cfg, schemaConfig, c)
	}

	if f.gcsObjectClient {
		var c *GCSObjectClient
		c, err = newGCSObjectClient(ctx, GCSConfig{
			BucketName: "chunks",
			Insecure:   true,
		}, hedging.Config{}, func(_ context.Context, _ ...option.ClientOption) (*storage.Client, error) {
			return f.gcssrv.Client(), nil
		})
		if err != nil {
			return
		}
		cClient = client.NewClient(c, nil, config.SchemaConfig{})
	} else {
		cClient = newBigtableObjectClient(Config{}, schemaConfig, c)
	}

	closer = testutils.CloserFunc(func() error {
		conn.Close()
		return nil
	})

	return
}

// Fixtures for unit testing GCP storage.
var Fixtures = func() []testutils.Fixture {
	fixtures := []testutils.Fixture{}
	for _, gcsObjectClient := range []bool{true, false} {
		for _, columnKeyClient := range []bool{true, false} {
			for _, hashPrefix := range []bool{true, false} {
				fixtures = append(fixtures, &fixture{
					name:            fmt.Sprintf("bigtable-columnkey:%v-gcsObjectClient:%v-hashPrefix:%v", columnKeyClient, gcsObjectClient, hashPrefix),
					columnKeyClient: columnKeyClient,
					gcsObjectClient: gcsObjectClient,
					hashPrefix:      hashPrefix,
				})
			}
		}
	}
	return fixtures
}()

package aws

import (
	"fmt"
	"io"
	"time"

	"golang.org/x/time/rate"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/objectclient"
	"github.com/cortexproject/cortex/pkg/chunk/testutils"
	"github.com/cortexproject/cortex/pkg/util"
)

type fixture struct {
	name    string
	clients func() (chunk.IndexClient, chunk.Client, chunk.TableClient, chunk.SchemaConfig, io.Closer, error)
}

func (f fixture) Name() string {
	return f.name
}

func (f fixture) Clients() (chunk.IndexClient, chunk.Client, chunk.TableClient, chunk.SchemaConfig, io.Closer, error) {
	return f.clients()
}

// Fixtures for testing the various configuration of AWS storage.
var Fixtures = []testutils.Fixture{
	fixture{
		name: "S3 chunks",
		clients: func() (chunk.IndexClient, chunk.Client, chunk.TableClient, chunk.SchemaConfig, io.Closer, error) {
			schemaConfig := testutils.DefaultSchemaConfig("s3")
			dynamoDB := newMockDynamoDB(0, 0)
			table := &dynamoTableClient{
				DynamoDB: dynamoDB,
				metrics:  newMetrics(nil),
			}
			index := &dynamoDBStorageClient{
				DynamoDB:                dynamoDB,
				batchGetItemRequestFn:   dynamoDB.batchGetItemRequest,
				batchWriteItemRequestFn: dynamoDB.batchWriteItemRequest,
				schemaCfg:               schemaConfig,
				metrics:                 newMetrics(nil),
			}
			object := objectclient.NewClient(&S3ObjectClient{S3: newMockS3()}, nil)
			return index, object, table, schemaConfig, testutils.CloserFunc(func() error {
				table.Stop()
				index.Stop()
				object.Stop()
				return nil
			}), nil
		},
	},
	dynamoDBFixture(0, 10, 20),
	dynamoDBFixture(0, 0, 20),
	dynamoDBFixture(2, 10, 20),
}

func dynamoDBFixture(provisionedErr, gangsize, maxParallelism int) testutils.Fixture {
	return fixture{
		name: fmt.Sprintf("DynamoDB chunks provisionedErr=%d, ChunkGangSize=%d, ChunkGetMaxParallelism=%d",
			provisionedErr, gangsize, maxParallelism),
		clients: func() (chunk.IndexClient, chunk.Client, chunk.TableClient, chunk.SchemaConfig, io.Closer, error) {
			dynamoDB := newMockDynamoDB(0, provisionedErr)
			schemaCfg := testutils.DefaultSchemaConfig("aws")
			table := &dynamoTableClient{
				DynamoDB: dynamoDB,
				metrics:  newMetrics(nil),
			}
			storage := &dynamoDBStorageClient{
				cfg: DynamoDBConfig{
					ChunkGangSize:          gangsize,
					ChunkGetMaxParallelism: maxParallelism,
					BackoffConfig: util.BackoffConfig{
						MinBackoff: 1 * time.Millisecond,
						MaxBackoff: 5 * time.Millisecond,
						MaxRetries: 20,
					},
				},
				DynamoDB:                dynamoDB,
				writeThrottle:           rate.NewLimiter(10, dynamoDBMaxWriteBatchSize),
				batchGetItemRequestFn:   dynamoDB.batchGetItemRequest,
				batchWriteItemRequestFn: dynamoDB.batchWriteItemRequest,
				schemaCfg:               schemaCfg,
				metrics:                 newMetrics(nil),
			}
			return storage, storage, table, schemaCfg, testutils.CloserFunc(func() error {
				table.Stop()
				storage.Stop()
				return nil
			}), nil
		},
	}
}

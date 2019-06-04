package aws

import (
	"fmt"
	"time"

	"golang.org/x/time/rate"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/testutils"
	"github.com/cortexproject/cortex/pkg/util"
)

type fixture struct {
	name    string
	clients func() (chunk.IndexClient, chunk.ObjectClient, chunk.TableClient, chunk.SchemaConfig, error)
}

func (f fixture) Name() string {
	return f.name
}

func (f fixture) Clients() (chunk.IndexClient, chunk.ObjectClient, chunk.TableClient, chunk.SchemaConfig, error) {
	return f.clients()
}

func (f fixture) Teardown() error {
	return nil
}

// Fixtures for testing the various configuration of AWS storage.
var Fixtures = []testutils.Fixture{
	fixture{
		name: "S3 chunks",
		clients: func() (chunk.IndexClient, chunk.ObjectClient, chunk.TableClient, chunk.SchemaConfig, error) {
			schemaConfig := testutils.DefaultSchemaConfig("s3")
			dynamoDB := newMockDynamoDB(0, 0)
			table := &dynamoTableClient{
				DynamoDB: dynamoDB,
			}
			index := &dynamoDBStorageClient{
				DynamoDB:                dynamoDB,
				queryRequestFn:          dynamoDB.queryRequest,
				batchGetItemRequestFn:   dynamoDB.batchGetItemRequest,
				batchWriteItemRequestFn: dynamoDB.batchWriteItemRequest,
				schemaCfg:               schemaConfig,
			}
			object := &s3ObjectClient{
				S3: newMockS3(),
			}
			return index, object, table, schemaConfig, nil
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
		clients: func() (chunk.IndexClient, chunk.ObjectClient, chunk.TableClient, chunk.SchemaConfig, error) {
			dynamoDB := newMockDynamoDB(0, provisionedErr)
			schemaCfg := testutils.DefaultSchemaConfig("aws")
			table := &dynamoTableClient{
				DynamoDB: dynamoDB,
			}
			storage := &dynamoDBStorageClient{
				cfg: DynamoDBConfig{
					ChunkGangSize:          gangsize,
					ChunkGetMaxParallelism: maxParallelism,
					backoffConfig: util.BackoffConfig{
						MinBackoff: 1 * time.Millisecond,
						MaxBackoff: 5 * time.Millisecond,
						MaxRetries: 20,
					},
				},
				DynamoDB:                dynamoDB,
				writeThrottle:           rate.NewLimiter(10, dynamoDBMaxWriteBatchSize),
				queryRequestFn:          dynamoDB.queryRequest,
				batchGetItemRequestFn:   dynamoDB.batchGetItemRequest,
				batchWriteItemRequestFn: dynamoDB.batchWriteItemRequest,
				schemaCfg:               schemaCfg,
			}
			return storage, storage, table, schemaCfg, nil
		},
	}
}

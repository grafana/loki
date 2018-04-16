package aws

import (
	"fmt"
	"time"

	"github.com/prometheus/common/model"
	"github.com/weaveworks/cortex/pkg/chunk"
	"github.com/weaveworks/cortex/pkg/util"
)

type fixture struct {
	name    string
	clients func() (chunk.StorageClient, chunk.TableClient, chunk.SchemaConfig, error)
}

func (f fixture) Name() string {
	return f.name
}

func (f fixture) Clients() (chunk.StorageClient, chunk.TableClient, chunk.SchemaConfig, error) {
	return f.clients()
}

func (f fixture) Teardown() error {
	return nil
}

// Fixtures for testing the various configuration of AWS storage.
var Fixtures = []chunk.Fixture{
	fixture{
		name: "S3 chunks",
		clients: func() (chunk.StorageClient, chunk.TableClient, chunk.SchemaConfig, error) {
			schemaConfig := chunk.SchemaConfig{} // Defaults == S3
			dynamoDB := newMockDynamoDB(0, 0)
			table := &dynamoTableClient{
				DynamoDB: dynamoDB,
			}
			storage := &storageClient{
				DynamoDB:                dynamoDB,
				S3:                      newMockS3(),
				queryRequestFn:          dynamoDB.queryRequest,
				batchGetItemRequestFn:   dynamoDB.batchGetItemRequest,
				batchWriteItemRequestFn: dynamoDB.batchWriteItemRequest,
				schemaCfg:               schemaConfig,
			}
			return storage, table, schemaConfig, nil
		},
	},
	dynamoDBFixture(0, 10, 20),
	dynamoDBFixture(0, 0, 20),
	dynamoDBFixture(2, 10, 20),
}

func dynamoDBFixture(provisionedErr, gangsize, maxParallelism int) chunk.Fixture {
	return fixture{
		name: fmt.Sprintf("DynamoDB chunks provisionedErr=%d, ChunkGangSize=%d, ChunkGetMaxParallelism=%d",
			provisionedErr, gangsize, maxParallelism),
		clients: func() (chunk.StorageClient, chunk.TableClient, chunk.SchemaConfig, error) {
			dynamoDB := newMockDynamoDB(0, provisionedErr)
			schemaCfg := chunk.SchemaConfig{
				ChunkTables: chunk.PeriodicTableConfig{
					From:   util.NewDayValue(model.Now()),
					Period: 10 * time.Minute,
					Prefix: "chunks",
				},
			}
			table := &dynamoTableClient{
				DynamoDB: dynamoDB,
			}
			storage := &storageClient{
				cfg: StorageConfig{
					DynamoDBConfig: DynamoDBConfig{
						ChunkGangSize:          gangsize,
						ChunkGetMaxParallelism: maxParallelism,
						backoffConfig: util.BackoffConfig{
							MinBackoff: 1 * time.Millisecond,
							MaxBackoff: 5 * time.Millisecond,
							MaxRetries: 20,
						},
					},
				},
				DynamoDB:                dynamoDB,
				S3:                      newMockS3(),
				queryRequestFn:          dynamoDB.queryRequest,
				batchGetItemRequestFn:   dynamoDB.batchGetItemRequest,
				batchWriteItemRequestFn: dynamoDB.batchWriteItemRequest,
				schemaCfg:               schemaCfg,
			}
			return storage, table, schemaCfg, nil
		},
	}
}

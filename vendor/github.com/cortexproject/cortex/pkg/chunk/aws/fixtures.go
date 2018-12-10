package aws

import (
	"fmt"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/testutils"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/prometheus/common/model"
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
			schemaConfig := chunk.SchemaConfig{} // Defaults == S3
			dynamoDB := newMockDynamoDB(0, 0)
			table := &dynamoTableClient{
				DynamoDB: dynamoDB,
			}
			index := &DynamoDBStorageClient{
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
			schemaCfg := chunk.SchemaConfig{
				Configs: []chunk.PeriodConfig{{
					IndexType: "aws",
					From:      model.Now(),
					ChunkTables: chunk.PeriodicTableConfig{
						Prefix: "chunks",
						Period: 10 * time.Minute,
					},
				}},
			}
			table := &dynamoTableClient{
				DynamoDB: dynamoDB,
			}
			storage := &DynamoDBStorageClient{
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
				queryRequestFn:          dynamoDB.queryRequest,
				batchGetItemRequestFn:   dynamoDB.batchGetItemRequest,
				batchWriteItemRequestFn: dynamoDB.batchWriteItemRequest,
				schemaCfg:               schemaCfg,
			}
			return storage, storage, table, schemaCfg, nil
		},
	}
}

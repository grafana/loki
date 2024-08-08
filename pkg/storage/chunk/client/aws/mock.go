package aws

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/go-kit/log/level"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const arnPrefix = "arn:"

type mockDynamoDBClient struct {
	dynamodbiface.DynamoDBAPI

	mtx            sync.RWMutex
	unprocessed    int
	provisionedErr int
	errAfter       int
	tables         map[string]*mockDynamoDBTable
}

type mockDynamoDBTable struct {
	items       map[string][]mockDynamoDBItem
	read, write int64
	tags        []*dynamodb.Tag
}

type mockDynamoDBItem map[string]*dynamodb.AttributeValue

// nolint
func newMockDynamoDB(unprocessed int, provisionedErr int) *mockDynamoDBClient {
	return &mockDynamoDBClient{
		tables:         map[string]*mockDynamoDBTable{},
		unprocessed:    unprocessed,
		provisionedErr: provisionedErr,
	}
}

func (a dynamoDBStorageClient) setErrorParameters(provisionedErr, errAfter int) {
	if m, ok := a.DynamoDB.(*mockDynamoDBClient); ok {
		m.provisionedErr = provisionedErr
		m.errAfter = errAfter
	}
}

//nolint:unused //Leaving this around in the case we need to create a table via mock this is useful.
func (m *mockDynamoDBClient) createTable(name string) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.tables[name] = &mockDynamoDBTable{
		items: map[string][]mockDynamoDBItem{},
	}
}

func (m *mockDynamoDBClient) batchWriteItemRequest(_ context.Context, input *dynamodb.BatchWriteItemInput) dynamoDBRequest {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	resp := &dynamodb.BatchWriteItemOutput{
		UnprocessedItems: map[string][]*dynamodb.WriteRequest{},
	}

	if m.errAfter > 0 {
		m.errAfter--
	} else if m.provisionedErr > 0 {
		m.provisionedErr--
		return &dynamoDBMockRequest{
			result: resp,
			err:    awserr.New(dynamodb.ErrCodeProvisionedThroughputExceededException, "", nil),
		}
	}

	for tableName, writeRequests := range input.RequestItems {
		table, ok := m.tables[tableName]
		if !ok {
			return &dynamoDBMockRequest{
				result: &dynamodb.BatchWriteItemOutput{},
				err:    fmt.Errorf("table not found: %s", tableName),
			}
		}

		for _, writeRequest := range writeRequests {
			if m.unprocessed > 0 {
				m.unprocessed--
				resp.UnprocessedItems[tableName] = append(resp.UnprocessedItems[tableName], writeRequest)
				continue
			}

			hashValue := *writeRequest.PutRequest.Item[hashKey].S
			rangeValue := writeRequest.PutRequest.Item[rangeKey].B

			items := table.items[hashValue]

			// insert in order
			i := sort.Search(len(items), func(i int) bool {
				return bytes.Compare(items[i][rangeKey].B, rangeValue) >= 0
			})
			if i >= len(items) || !bytes.Equal(items[i][rangeKey].B, rangeValue) {
				items = append(items, nil)
				copy(items[i+1:], items[i:])
			} else {
				return &dynamoDBMockRequest{
					result: &dynamodb.BatchWriteItemOutput{},
					err:    fmt.Errorf("Duplicate entry"),
				}
			}
			items[i] = writeRequest.PutRequest.Item

			table.items[hashValue] = items
		}
	}
	return &dynamoDBMockRequest{result: resp}
}

func (m *mockDynamoDBClient) batchGetItemRequest(_ context.Context, input *dynamodb.BatchGetItemInput) dynamoDBRequest {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	resp := &dynamodb.BatchGetItemOutput{
		Responses:       map[string][]map[string]*dynamodb.AttributeValue{},
		UnprocessedKeys: map[string]*dynamodb.KeysAndAttributes{},
	}

	if m.errAfter > 0 {
		m.errAfter--
	} else if m.provisionedErr > 0 {
		m.provisionedErr--
		return &dynamoDBMockRequest{
			result: resp,
			err:    awserr.New(dynamodb.ErrCodeProvisionedThroughputExceededException, "", nil),
		}
	}

	for tableName, readRequests := range input.RequestItems {
		table, ok := m.tables[tableName]
		if !ok {
			return &dynamoDBMockRequest{
				result: &dynamodb.BatchGetItemOutput{},
				err:    fmt.Errorf("table not found"),
			}
		}

		unprocessed := &dynamodb.KeysAndAttributes{
			AttributesToGet:          readRequests.AttributesToGet,
			ConsistentRead:           readRequests.ConsistentRead,
			ExpressionAttributeNames: readRequests.ExpressionAttributeNames,
		}
		for _, readRequest := range readRequests.Keys {
			if m.unprocessed > 0 {
				m.unprocessed--
				unprocessed.Keys = append(unprocessed.Keys, readRequest)
				resp.UnprocessedKeys[tableName] = unprocessed
				continue
			}

			hashValue := *readRequest[hashKey].S
			rangeValue := readRequest[rangeKey].B
			items := table.items[hashValue]

			// insert in order
			i := sort.Search(len(items), func(i int) bool {
				return bytes.Compare(items[i][rangeKey].B, rangeValue) >= 0
			})
			if i >= len(items) || !bytes.Equal(items[i][rangeKey].B, rangeValue) {
				return &dynamoDBMockRequest{
					result: &dynamodb.BatchGetItemOutput{},
					err:    fmt.Errorf("Couldn't find item"),
				}
			}

			// Only return AttributesToGet!
			item := map[string]*dynamodb.AttributeValue{}
			for _, key := range readRequests.AttributesToGet {
				item[*key] = items[i][*key]
			}
			resp.Responses[tableName] = append(resp.Responses[tableName], item)
		}
	}
	return &dynamoDBMockRequest{
		result: resp,
	}
}

func (m *mockDynamoDBClient) QueryPagesWithContext(_ aws.Context, input *dynamodb.QueryInput, fn func(*dynamodb.QueryOutput, bool) bool, _ ...request.Option) error {
	result := &dynamodb.QueryOutput{
		Items: []map[string]*dynamodb.AttributeValue{},
	}

	// Required filters
	hashValue := *input.KeyConditions[hashKey].AttributeValueList[0].S

	// Optional filters
	var (
		rangeValueFilter     []byte
		rangeValueFilterType string
	)
	if c, ok := input.KeyConditions[rangeKey]; ok {
		rangeValueFilter = c.AttributeValueList[0].B
		rangeValueFilterType = *c.ComparisonOperator
	}

	// Filter by HashValue, RangeValue and Value if it exists
	items := m.tables[*input.TableName].items[hashValue]
	for _, item := range items {
		rangeValue := item[rangeKey].B
		if rangeValueFilterType == dynamodb.ComparisonOperatorGe && bytes.Compare(rangeValue, rangeValueFilter) < 0 {
			continue
		}
		if rangeValueFilterType == dynamodb.ComparisonOperatorBeginsWith && !bytes.HasPrefix(rangeValue, rangeValueFilter) {
			continue
		}

		if item[valueKey] != nil {
			value := item[valueKey].B

			// Apply filterExpression if it exists (supporting only v = :v)
			if input.FilterExpression != nil {
				if *input.FilterExpression == fmt.Sprintf("%s = :v", valueKey) {
					filterValue := input.ExpressionAttributeValues[":v"].B
					if !bytes.Equal(value, filterValue) {
						continue
					}
				} else {
					level.Warn(util_log.Logger).Log("msg", "unsupported FilterExpression", "expression", *input.FilterExpression)
				}
			}
		}

		result.Items = append(result.Items, item)
	}
	fn(result, true)
	return nil
}

type dynamoDBMockRequest struct {
	result interface{}
	err    error
}

func (m *dynamoDBMockRequest) Send() error {
	return m.err
}

func (m *dynamoDBMockRequest) Data() interface{} {
	return m.result
}

func (m *dynamoDBMockRequest) Error() error {
	return m.err
}

func (m *dynamoDBMockRequest) Retryable() bool {
	return false
}

func (m *mockDynamoDBClient) ListTablesPagesWithContext(_ aws.Context, _ *dynamodb.ListTablesInput, fn func(*dynamodb.ListTablesOutput, bool) bool, _ ...request.Option) error {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	var tableNames []*string
	for tableName := range m.tables {
		func(tableName string) {
			tableNames = append(tableNames, &tableName)
		}(tableName)
	}
	fn(&dynamodb.ListTablesOutput{
		TableNames: tableNames,
	}, true)

	return nil
}

// CreateTable implements StorageClient.
func (m *mockDynamoDBClient) CreateTableWithContext(_ aws.Context, input *dynamodb.CreateTableInput, _ ...request.Option) (*dynamodb.CreateTableOutput, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if _, ok := m.tables[*input.TableName]; ok {
		return nil, fmt.Errorf("table already exists")
	}

	m.tables[*input.TableName] = &mockDynamoDBTable{
		items: map[string][]mockDynamoDBItem{},
		write: *input.ProvisionedThroughput.WriteCapacityUnits,
		read:  *input.ProvisionedThroughput.ReadCapacityUnits,
	}

	return &dynamodb.CreateTableOutput{
		TableDescription: &dynamodb.TableDescription{
			TableArn: aws.String(arnPrefix + *input.TableName),
		},
	}, nil
}

// DescribeTable implements StorageClient.
func (m *mockDynamoDBClient) DescribeTableWithContext(_ aws.Context, input *dynamodb.DescribeTableInput, _ ...request.Option) (*dynamodb.DescribeTableOutput, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	table, ok := m.tables[*input.TableName]
	if !ok {
		return nil, fmt.Errorf("not found")
	}

	return &dynamodb.DescribeTableOutput{
		Table: &dynamodb.TableDescription{
			TableName:   input.TableName,
			TableStatus: aws.String(dynamodb.TableStatusActive),
			ProvisionedThroughput: &dynamodb.ProvisionedThroughputDescription{
				ReadCapacityUnits:  aws.Int64(table.read),
				WriteCapacityUnits: aws.Int64(table.write),
			},
			TableArn: aws.String(arnPrefix + *input.TableName),
		},
	}, nil
}

// UpdateTable implements StorageClient.
func (m *mockDynamoDBClient) UpdateTableWithContext(_ aws.Context, input *dynamodb.UpdateTableInput, _ ...request.Option) (*dynamodb.UpdateTableOutput, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	table, ok := m.tables[*input.TableName]
	if !ok {
		return nil, fmt.Errorf("not found")
	}

	table.read = *input.ProvisionedThroughput.ReadCapacityUnits
	table.write = *input.ProvisionedThroughput.WriteCapacityUnits

	return &dynamodb.UpdateTableOutput{
		TableDescription: &dynamodb.TableDescription{
			TableArn: aws.String(arnPrefix + *input.TableName),
		},
	}, nil
}

func (m *mockDynamoDBClient) TagResourceWithContext(_ aws.Context, input *dynamodb.TagResourceInput, _ ...request.Option) (*dynamodb.TagResourceOutput, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	if len(input.Tags) == 0 {
		return nil, fmt.Errorf("tags are required")
	}

	if !strings.HasPrefix(*input.ResourceArn, arnPrefix) {
		return nil, fmt.Errorf("not an arn: %v", *input.ResourceArn)
	}

	table, ok := m.tables[strings.TrimPrefix(*input.ResourceArn, arnPrefix)]
	if !ok {
		return nil, fmt.Errorf("not found")
	}

	table.tags = input.Tags
	return &dynamodb.TagResourceOutput{}, nil
}

func (m *mockDynamoDBClient) ListTagsOfResourceWithContext(_ aws.Context, input *dynamodb.ListTagsOfResourceInput, _ ...request.Option) (*dynamodb.ListTagsOfResourceOutput, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	if !strings.HasPrefix(*input.ResourceArn, arnPrefix) {
		return nil, fmt.Errorf("not an arn: %v", *input.ResourceArn)
	}

	table, ok := m.tables[strings.TrimPrefix(*input.ResourceArn, arnPrefix)]
	if !ok {
		return nil, fmt.Errorf("not found")
	}

	return &dynamodb.ListTagsOfResourceOutput{
		Tags: table.tags,
	}, nil
}

type mockS3 struct {
	s3iface.S3API
	sync.RWMutex
	objects map[string][]byte
}

func newMockS3() *mockS3 {
	return &mockS3{
		objects: map[string][]byte{},
	}
}

func (m *mockS3) PutObjectWithContext(_ aws.Context, req *s3.PutObjectInput, _ ...request.Option) (*s3.PutObjectOutput, error) {
	m.Lock()
	defer m.Unlock()

	buf, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}

	m.objects[*req.Key] = buf
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3) GetObjectWithContext(_ aws.Context, req *s3.GetObjectInput, _ ...request.Option) (*s3.GetObjectOutput, error) {
	m.RLock()
	defer m.RUnlock()

	buf, ok := m.objects[*req.Key]
	if !ok {
		return nil, fmt.Errorf("Not found")
	}

	return &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(buf)),
	}, nil
}

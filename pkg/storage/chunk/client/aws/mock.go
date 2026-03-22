package aws

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/go-kit/log/level"

	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

const arnPrefix = "arn:"

type mockDynamoDBClient struct {
	dynamodb.Client

	mtx            sync.RWMutex
	unprocessed    int
	provisionedErr int
	errAfter       int
	tables         map[string]*mockDynamoDBTable
}

type mockDynamoDBTable struct {
	items       map[string][]mockDynamoDBItem
	read, write int64
	tags        []types.Tag
}

type mockDynamoDBItem map[string]types.AttributeValue

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

func (m *mockDynamoDBClient) BatchWriteItem(_ context.Context, params *dynamodb.BatchWriteItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	resp := &dynamodb.BatchWriteItemOutput{
		UnprocessedItems: map[string][]types.WriteRequest{},
	}

	if m.errAfter > 0 {
		m.errAfter--
	} else if m.provisionedErr > 0 {
		m.provisionedErr--
		return resp, &types.ProvisionedThroughputExceededException{}
	}

	for tableName, writeRequests := range params.RequestItems {
		table, ok := m.tables[tableName]
		if !ok {
			return &dynamodb.BatchWriteItemOutput{}, fmt.Errorf("table not found: %s", tableName)
		}

		for _, writeRequest := range writeRequests {
			if m.unprocessed > 0 {
				m.unprocessed--
				resp.UnprocessedItems[tableName] = append(resp.UnprocessedItems[tableName], writeRequest)
				continue
			}

			hashValue := writeRequest.PutRequest.Item[hashKey].(*types.AttributeValueMemberS).Value
			rangeValue := writeRequest.PutRequest.Item[rangeKey].(*types.AttributeValueMemberB).Value

			items := table.items[hashValue]

			// insert in order
			i := sort.Search(len(items), func(i int) bool {
				return bytes.Compare(items[i][rangeKey].(*types.AttributeValueMemberB).Value, rangeValue) >= 0
			})
			if i >= len(items) || !bytes.Equal(items[i][rangeKey].(*types.AttributeValueMemberB).Value, rangeValue) {
				items = append(items, nil)
				copy(items[i+1:], items[i:])
			} else {
				return &dynamodb.BatchWriteItemOutput{}, fmt.Errorf("duplicate entry")
			}
			items[i] = writeRequest.PutRequest.Item

			table.items[hashValue] = items
		}
	}
	return resp, nil
}

func (m *mockDynamoDBClient) BatchGetItem(_ context.Context, params *dynamodb.BatchGetItemInput, _ ...func(*dynamodb.Options)) (*dynamodb.BatchGetItemOutput, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	resp := &dynamodb.BatchGetItemOutput{
		Responses:       map[string][]map[string]types.AttributeValue{},
		UnprocessedKeys: map[string]types.KeysAndAttributes{},
	}

	if m.errAfter > 0 {
		m.errAfter--
	} else if m.provisionedErr > 0 {
		m.provisionedErr--
		return resp, &types.ProvisionedThroughputExceededException{}
	}

	for tableName, readRequests := range params.RequestItems {
		table, ok := m.tables[tableName]
		if !ok {
			return &dynamodb.BatchGetItemOutput{}, fmt.Errorf("table not found")
		}

		unprocessed := &types.KeysAndAttributes{
			AttributesToGet:          readRequests.AttributesToGet,
			ConsistentRead:           readRequests.ConsistentRead,
			ExpressionAttributeNames: readRequests.ExpressionAttributeNames,
		}
		for _, readRequest := range readRequests.Keys {
			if m.unprocessed > 0 {
				m.unprocessed--
				unprocessed.Keys = append(unprocessed.Keys, readRequest)
				resp.UnprocessedKeys[tableName] = *unprocessed
				continue
			}

			hashValue := readRequest[hashKey].(*types.AttributeValueMemberS).Value
			rangeValue := readRequest[rangeKey].(*types.AttributeValueMemberB).Value
			items := table.items[hashValue]

			// insert in order
			i := sort.Search(len(items), func(i int) bool {
				return bytes.Compare(items[i][rangeKey].(*types.AttributeValueMemberB).Value, rangeValue) >= 0
			})
			if i >= len(items) || !bytes.Equal(items[i][rangeKey].(*types.AttributeValueMemberB).Value, rangeValue) {
				return &dynamodb.BatchGetItemOutput{}, fmt.Errorf("couldn't find item")
			}

			// Only return AttributesToGet!
			item := map[string]types.AttributeValue{}
			for _, key := range readRequests.AttributesToGet {
				item[key] = items[i][key]
			}
			resp.Responses[tableName] = append(resp.Responses[tableName], item)
		}
	}
	return resp, nil
}

func (m *mockDynamoDBClient) Query(_ context.Context, params *dynamodb.QueryInput, _ ...func(*dynamodb.Options)) (*dynamodb.QueryOutput, error) {
	result := &dynamodb.QueryOutput{
		Items: []map[string]types.AttributeValue{},
	}

	// Required filters
	hashValue := params.KeyConditions[hashKey].AttributeValueList[0].(*types.AttributeValueMemberS).Value

	// Optional filters
	var (
		rangeValueFilter     []byte
		rangeValueFilterType string
	)
	if c, ok := params.KeyConditions[rangeKey]; ok {
		rangeValueFilter = c.AttributeValueList[0].(*types.AttributeValueMemberB).Value
		rangeValueFilterType = string(c.ComparisonOperator)
	}

	// Filter by HashValue, RangeValue and Value if it exists
	items := m.tables[*params.TableName].items[hashValue]
	for _, item := range items {
		rangeValue := (item[rangeKey]).(*types.AttributeValueMemberB).Value
		if rangeValueFilterType == string(types.ComparisonOperatorGe) && bytes.Compare(rangeValue, rangeValueFilter) < 0 {
			continue
		}
		if rangeValueFilterType == string(types.ComparisonOperatorBeginsWith) && !bytes.HasPrefix(rangeValue, rangeValueFilter) {
			continue
		}

		if item[valueKey] != nil {
			value := (item[valueKey]).(*types.AttributeValueMemberB).Value

			// Apply filterExpression if it exists (supporting only v = :v)
			if params.FilterExpression != nil {
				if *params.FilterExpression == fmt.Sprintf("%s = :v", valueKey) {
					filterValue := params.ExpressionAttributeValues[":v"].(*types.AttributeValueMemberB).Value
					if !bytes.Equal(value, filterValue) {
						continue
					}
				} else {
					level.Warn(util_log.Logger).Log("msg", "unsupported FilterExpression", "expression", *params.FilterExpression)
				}
			}
		}

		result.Items = append(result.Items, item)
	}
	return result, nil
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

func (m *mockDynamoDBClient) ListTables(_ context.Context, _ *dynamodb.ListTablesInput, _ ...func(*dynamodb.Options)) (*dynamodb.ListTablesOutput, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	var tableNames []string
	for tableName := range m.tables {
		func(tableName string) {
			tableNames = append(tableNames, tableName)
		}(tableName)
	}

	return &dynamodb.ListTablesOutput{TableNames: tableNames}, nil
}

// CreateTable implements StorageClient.
func (m *mockDynamoDBClient) CreateTable(_ context.Context, input *dynamodb.CreateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error) {
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
		TableDescription: &types.TableDescription{
			TableArn: aws.String(arnPrefix + *input.TableName),
		},
	}, nil
}

// DescribeTable implements StorageClient.
func (m *mockDynamoDBClient) DescribeTable(_ context.Context, input *dynamodb.DescribeTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.DescribeTableOutput, error) {
	m.mtx.RLock()
	defer m.mtx.RUnlock()

	table, ok := m.tables[*input.TableName]
	if !ok {
		return nil, fmt.Errorf("not found")
	}

	return &dynamodb.DescribeTableOutput{
		Table: &types.TableDescription{
			TableName:   input.TableName,
			TableStatus: types.TableStatusActive,
			ProvisionedThroughput: &types.ProvisionedThroughputDescription{
				ReadCapacityUnits:  aws.Int64(table.read),
				WriteCapacityUnits: aws.Int64(table.write),
			},
			TableArn: aws.String(arnPrefix + *input.TableName),
		},
	}, nil
}

// UpdateTable implements StorageClient.
func (m *mockDynamoDBClient) UpdateTable(_ context.Context, input *dynamodb.UpdateTableInput, _ ...func(*dynamodb.Options)) (*dynamodb.UpdateTableOutput, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	table, ok := m.tables[*input.TableName]
	if !ok {
		return nil, fmt.Errorf("not found")
	}

	table.read = *input.ProvisionedThroughput.ReadCapacityUnits
	table.write = *input.ProvisionedThroughput.WriteCapacityUnits

	return &dynamodb.UpdateTableOutput{
		TableDescription: &types.TableDescription{
			TableArn: aws.String(arnPrefix + *input.TableName),
		},
	}, nil
}

func (m *mockDynamoDBClient) TagResource(_ context.Context, input *dynamodb.TagResourceInput, _ ...func(*dynamodb.Options)) (*dynamodb.TagResourceOutput, error) {
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

func (m *mockDynamoDBClient) ListTagsOfResource(_ context.Context, input *dynamodb.ListTagsOfResourceInput, _ ...func(*dynamodb.Options)) (*dynamodb.ListTagsOfResourceOutput, error) {
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
	s3.Client
	sync.RWMutex
	objects map[string][]byte
}

func newMockS3() mockS3 {
	return mockS3{
		objects: map[string][]byte{},
	}
}

func (m *mockS3) PutObject(_ context.Context, params *s3.PutObjectInput, _ ...func(*s3.Options)) (*s3.PutObjectOutput, error) {
	m.Lock()
	defer m.Unlock()

	buf, err := io.ReadAll(params.Body)
	if err != nil {
		return nil, err
	}

	m.objects[*params.Key] = buf
	return &s3.PutObjectOutput{}, nil
}

func (m *mockS3) GetObject(_ context.Context, params *s3.GetObjectInput, _ ...func(*s3.Options)) (*s3.GetObjectOutput, error) {
	m.RLock()
	defer m.RUnlock()

	buf, ok := m.objects[*params.Key]
	if !ok {
		return nil, fmt.Errorf("not found")
	}

	return &s3.GetObjectOutput{
		Body: io.NopCloser(bytes.NewReader(buf)),
	}, nil
}

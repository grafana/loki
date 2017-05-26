package chunk

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/prometheus/common/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"

	"github.com/weaveworks/cortex/pkg/util"
)

const arnPrefix = "arn:"

type mockDynamoDBClient struct {
	dynamodbiface.DynamoDBAPI

	mtx            sync.RWMutex
	unprocessed    int
	provisionedErr int
	tables         map[string]*mockDynamoDBTable
}

type mockDynamoDBTable struct {
	items       map[string][]mockDynamoDBItem
	read, write int64
	tags        []*dynamodb.Tag
}

type mockDynamoDBItem map[string]*dynamodb.AttributeValue

func newMockDynamoDB(unprocessed int, provisionedErr int) *mockDynamoDBClient {
	return &mockDynamoDBClient{
		tables:         map[string]*mockDynamoDBTable{},
		unprocessed:    unprocessed,
		provisionedErr: provisionedErr,
	}
}

func (m *mockDynamoDBClient) createTable(name string) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.tables[name] = &mockDynamoDBTable{
		items: map[string][]mockDynamoDBItem{},
	}
}

func (m *mockDynamoDBClient) BatchWriteItemWithContext(_ aws.Context, input *dynamodb.BatchWriteItemInput, _ ...request.Option) (*dynamodb.BatchWriteItemOutput, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	resp := &dynamodb.BatchWriteItemOutput{
		UnprocessedItems: map[string][]*dynamodb.WriteRequest{},
	}

	if m.provisionedErr > 0 {
		m.provisionedErr--
		return resp, awserr.New(dynamodb.ErrCodeProvisionedThroughputExceededException, "", nil)
	}

	for tableName, writeRequests := range input.RequestItems {
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
				return &dynamodb.BatchWriteItemOutput{}, fmt.Errorf("Duplicate entry")
			}
			items[i] = writeRequest.PutRequest.Item

			table.items[hashValue] = items
		}
	}
	return resp, nil
}

func (m *mockDynamoDBClient) BatchGetItemWithContext(_ aws.Context, input *dynamodb.BatchGetItemInput, _ ...request.Option) (*dynamodb.BatchGetItemOutput, error) {
	m.mtx.Lock()
	defer m.mtx.Unlock()

	resp := &dynamodb.BatchGetItemOutput{
		Responses:       map[string][]map[string]*dynamodb.AttributeValue{},
		UnprocessedKeys: map[string]*dynamodb.KeysAndAttributes{},
	}

	if m.provisionedErr > 0 {
		m.provisionedErr--
		return resp, awserr.New(dynamodb.ErrCodeProvisionedThroughputExceededException, "", nil)
	}

	for tableName, readRequests := range input.RequestItems {
		table, ok := m.tables[tableName]
		if !ok {
			return &dynamodb.BatchGetItemOutput{}, fmt.Errorf("table not found")
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
				return &dynamodb.BatchGetItemOutput{}, fmt.Errorf("Couldn't find ite,")
			}

			// Only return AttributesToGet!
			item := map[string]*dynamodb.AttributeValue{}
			for _, key := range readRequests.AttributesToGet {
				item[*key] = items[i][*key]
			}
			resp.Responses[tableName] = append(resp.Responses[tableName], item)
		}
	}
	return resp, nil
}

func (m *mockDynamoDBClient) queryRequest(_ context.Context, input *dynamodb.QueryInput) dynamoDBRequest {
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
					log.Warnf("Unsupported FilterExpression: %s", *input.FilterExpression)
				}
			}
		}

		result.Items = append(result.Items, item)
	}

	return &dynamoDBMockRequest{
		result: result,
	}
}

type dynamoDBMockRequest struct {
	result *dynamodb.QueryOutput
}

func (m *dynamoDBMockRequest) NextPage() dynamoDBRequest {
	return m
}
func (m *dynamoDBMockRequest) Send() error {
	return nil
}
func (m *dynamoDBMockRequest) Data() interface{} {
	return m.result
}
func (m *dynamoDBMockRequest) Error() error {
	return nil
}
func (m *dynamoDBMockRequest) HasNextPage() bool {
	return false
}

func (m *mockDynamoDBClient) ListTablesPagesWithContext(_ aws.Context, input *dynamodb.ListTablesInput, fn func(*dynamodb.ListTablesOutput, bool) bool, _ ...request.Option) error {
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

	buf, err := ioutil.ReadAll(req.Body)
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
		Body: ioutil.NopCloser(bytes.NewReader(buf)),
	}, nil
}

func TestAWSStorageClient(t *testing.T) {
	dynamoDB := newMockDynamoDB(0, 0)
	client := awsStorageClient{
		DynamoDB:       dynamoDB,
		queryRequestFn: dynamoDB.queryRequest,
	}
	batch := client.NewWriteBatch()
	for i := 0; i < 30; i++ {
		batch.Add("table", fmt.Sprintf("hash%d", i), []byte(fmt.Sprintf("range%d", i)), nil)
	}
	dynamoDB.createTable("table")

	err := client.BatchWrite(context.Background(), batch)
	require.NoError(t, err)

	for i := 0; i < 30; i++ {
		entry := IndexQuery{
			TableName: "table",
			HashValue: fmt.Sprintf("hash%d", i),
		}
		var have []IndexEntry
		err := client.QueryPages(context.Background(), entry, func(read ReadBatch, lastPage bool) bool {
			for j := 0; j < read.Len(); j++ {
				have = append(have, IndexEntry{
					RangeValue: read.RangeValue(j),
				})
			}
			return !lastPage
		})
		require.NoError(t, err)
		require.Equal(t, []IndexEntry{
			{RangeValue: []byte(fmt.Sprintf("range%d", i))},
		}, have)
	}
}

func TestAWSStorageClientChunks(t *testing.T) {
	t.Run("S3 chunks", func(t *testing.T) {
		dynamoDB := newMockDynamoDB(0, 0)
		client := awsStorageClient{
			DynamoDB:       dynamoDB,
			S3:             newMockS3(),
			queryRequestFn: dynamoDB.queryRequest,
		}

		testStorageClientChunks(t, client)
	})

	t.Run("DynamoDB chunks", func(t *testing.T) {
		dynamoDB := newMockDynamoDB(0, 0)
		periodicChunkTableConfig := PeriodicChunkTableConfig{
			ChunkTableFrom:   util.NewDayValue(model.Now()),
			ChunkTablePeriod: 1 * time.Minute,
			ChunkTablePrefix: "chunks",
		}
		tableManager, err := NewTableManager(
			TableManagerConfig{
				PeriodicChunkTableConfig: periodicChunkTableConfig,
			},
			&dynamoTableClient{
				DynamoDB: dynamoDB,
			},
		)
		require.NoError(t, err)
		err = tableManager.syncTables(context.Background())
		require.NoError(t, err)

		client := awsStorageClient{
			DynamoDB:       dynamoDB,
			queryRequestFn: dynamoDB.queryRequest,
			cfg: AWSStorageConfig{
				PeriodicChunkTableConfig: periodicChunkTableConfig,
			},
		}

		testStorageClientChunks(t, client)
	})
}

func testStorageClientChunks(t *testing.T, client StorageClient) {
	const batchSize = 50

	// Write a few batches of chunks.
	written := []string{}
	for i := 0; i < 50; i++ {
		chunks := []Chunk{}
		for j := 0; j < batchSize; j++ {
			chunk := dummyChunkFor(model.Metric{
				model.MetricNameLabel: "foo",
				"index":               model.LabelValue(strconv.Itoa(i*batchSize + j)),
			})
			chunks = append(chunks, chunk)
			_, err := chunk.encode() // Need to encode it, side effect calculates crc
			require.NoError(t, err)
			written = append(written, chunk.externalKey())
		}
		err := client.PutChunks(context.Background(), chunks)
		require.NoError(t, err)
	}

	// Get a few batches of chunks.
	for i := 0; i < 50; i++ {
		chunksToGet := []Chunk{}
		for j := 0; j < batchSize; j++ {
			key := written[rand.Intn(len(written))]
			chunk, err := parseNewExternalKey(key)
			require.NoError(t, err)
			chunksToGet = append(chunksToGet, chunk)
		}

		chunksWeGot, err := client.GetChunks(context.Background(), chunksToGet)
		require.NoError(t, err)

		sort.Sort(ByKey(chunksToGet))
		sort.Sort(ByKey(chunksWeGot))
		require.Equal(t, len(chunksToGet), len(chunksWeGot))
		for j := 0; j < len(chunksWeGot); j++ {
			require.Equal(t, chunksToGet[i].externalKey(), chunksWeGot[i].externalKey())
		}
	}
}

func TestAWSStorageClientQueryPages(t *testing.T) {
	entries := []IndexEntry{
		{
			TableName:  "table",
			HashValue:  "foo",
			RangeValue: []byte("bar:1"),
			Value:      []byte("10"),
		},
		{
			TableName:  "table",
			HashValue:  "foo",
			RangeValue: []byte("bar:2"),
			Value:      []byte("20"),
		},
		{
			TableName:  "table",
			HashValue:  "foo",
			RangeValue: []byte("bar:3"),
			Value:      []byte("30"),
		},
		{
			TableName:  "table",
			HashValue:  "foo",
			RangeValue: []byte("baz:1"),
			Value:      []byte("10"),
		},
		{
			TableName:  "table",
			HashValue:  "foo",
			RangeValue: []byte("baz:2"),
			Value:      []byte("20"),
		},
		{
			TableName:  "table",
			HashValue:  "flip",
			RangeValue: []byte("bar:1"),
			Value:      []byte("abc"),
		},
		{
			TableName:  "table",
			HashValue:  "flip",
			RangeValue: []byte("bar:2"),
			Value:      []byte("abc"),
		},
		{
			TableName:  "table",
			HashValue:  "flip",
			RangeValue: []byte("bar:3"),
			Value:      []byte("abc"),
		},
	}

	tests := []struct {
		name  string
		query IndexQuery
		want  []IndexEntry
	}{
		{
			"check HashValue only",
			IndexQuery{
				TableName: "table",
				HashValue: "flip",
			},
			[]IndexEntry{entries[5], entries[6], entries[7]},
		},
		{
			"check RangeValueStart",
			IndexQuery{
				TableName:       "table",
				HashValue:       "foo",
				RangeValueStart: []byte("bar:2"),
			},
			[]IndexEntry{entries[1], entries[2], entries[3], entries[4]},
		},
		{
			"check RangeValuePrefix",
			IndexQuery{
				TableName:        "table",
				HashValue:        "foo",
				RangeValuePrefix: []byte("baz:"),
			},
			[]IndexEntry{entries[3], entries[4]},
		},
		{
			"check ValueEqual",
			IndexQuery{
				TableName:        "table",
				HashValue:        "foo",
				RangeValuePrefix: []byte("bar"),
				ValueEqual:       []byte("20"),
			},
			[]IndexEntry{entries[1]},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dynamoDB := newMockDynamoDB(0, 0)
			client := awsStorageClient{
				DynamoDB:       dynamoDB,
				queryRequestFn: dynamoDB.queryRequest,
			}

			batch := client.NewWriteBatch()
			for _, entry := range entries {
				batch.Add(entry.TableName, entry.HashValue, entry.RangeValue, entry.Value)
			}
			dynamoDB.createTable("table")

			err := client.BatchWrite(context.Background(), batch)
			require.NoError(t, err)

			var have []IndexEntry
			err = client.QueryPages(context.Background(), tt.query, func(read ReadBatch, lastPage bool) bool {
				for i := 0; i < read.Len(); i++ {
					have = append(have, IndexEntry{
						TableName:  tt.query.TableName,
						HashValue:  tt.query.HashValue,
						RangeValue: read.RangeValue(i),
						Value:      read.Value(i),
					})
				}
				return !lastPage
			})
			require.NoError(t, err)
			require.Equal(t, tt.want, have)
		})
	}
}

func TestAWSConfigFromURL(t *testing.T) {
	for _, tc := range []struct {
		url            string
		expectedKey    string
		expectedSecret string
		expectedRegion string
		expectedEp     string

		expectedNotSpecifiedUserErr bool
	}{
		{
			"s3://abc:123@s3.default.svc.cluster.local:4569",
			"abc",
			"123",
			"dummy",
			"http://s3.default.svc.cluster.local:4569",
			false,
		},
		{
			"dynamodb://user:pass@dynamodb.default.svc.cluster.local:8000/cortex",
			"user",
			"pass",
			"dummy",
			"http://dynamodb.default.svc.cluster.local:8000",
			false,
		},
		{
			// Not escaped password.
			"s3://abc:123/@s3.default.svc.cluster.local:4569",
			"",
			"",
			"",
			"",
			true,
		},
		{
			// Not escaped username.
			"s3://abc/:123@s3.default.svc.cluster.local:4569",
			"",
			"",
			"",
			"",
			true,
		},
		{
			"s3://keyWithEscapedSlashAtTheEnd%2F:%24%2C%26%2C%2B%2C%27%2C%2F%2C%3A%2C%3B%2C%3D%2C%3F%2C%40@eu-west-2/bucket1",
			"keyWithEscapedSlashAtTheEnd/",
			"$,&,+,',/,:,;,=,?,@",
			"eu-west-2",
			"",
			false,
		},
	} {
		parsedURL, err := url.Parse(tc.url)
		require.NoError(t, err)

		cfg, err := awsConfigFromURL(parsedURL)
		if tc.expectedNotSpecifiedUserErr {
			require.Error(t, err)
			continue
		}
		require.NoError(t, err)

		require.NotNil(t, cfg.Credentials)
		val, err := cfg.Credentials.Get()
		require.NoError(t, err)

		assert.Equal(t, tc.expectedKey, val.AccessKeyID)
		assert.Equal(t, tc.expectedSecret, val.SecretAccessKey)

		require.NotNil(t, cfg.Region)
		assert.Equal(t, tc.expectedRegion, *cfg.Region)

		if tc.expectedEp != "" {
			require.NotNil(t, cfg.Endpoint)
			assert.Equal(t, tc.expectedEp, *cfg.Endpoint)
		}
	}
}

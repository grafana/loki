package chunk

import (
	"bytes"
	"fmt"
	"net/url"
	"sort"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
	"github.com/prometheus/common/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

type mockDynamoDBClient struct {
	dynamodbiface.DynamoDBAPI

	mtx            sync.RWMutex
	unprocessed    int
	provisionedErr int
	tables         map[string]*mockDynamoDBTable
}

type mockDynamoDBTable struct {
	items map[string][]mockDynamoDBItem
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
		return resp, awserr.New(provisionedThroughputExceededException, "", nil)
	}

	for tableName, writeRequests := range input.RequestItems {
		table, ok := m.tables[tableName]
		if !ok {
			return &dynamodb.BatchWriteItemOutput{}, fmt.Errorf("table not found")
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

func TestDynamoDBClient(t *testing.T) {
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
			for i := 0; i < read.Len(); i++ {
				have = append(have, IndexEntry{
					RangeValue: read.RangeValue(i),
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

func TestDynamoDBClientQueryPages(t *testing.T) {
	dynamoDB := newMockDynamoDB(0, 0)
	client := awsStorageClient{
		DynamoDB:       dynamoDB,
		queryRequestFn: dynamoDB.queryRequest,
	}

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

	batch := client.NewWriteBatch()
	for _, entry := range entries {
		batch.Add(entry.TableName, entry.HashValue, entry.RangeValue, entry.Value)
	}
	dynamoDB.createTable("table")

	err := client.BatchWrite(context.Background(), batch)
	require.NoError(t, err)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var have []IndexEntry
			err := client.QueryPages(context.Background(), tt.query, func(read ReadBatch, lastPage bool) bool {
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

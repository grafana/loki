package aws

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
	"github.com/go-kit/log/level"
	awscommon "github.com/grafana/dskit/aws"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/instrument"
	ot "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	chunkclient "github.com/grafana/loki/v3/pkg/storage/chunk/client"
	client_util "github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/util/math"
	"github.com/grafana/loki/v3/pkg/util/spanlogger"
)

const (
	hashKey  = "h"
	rangeKey = "r"
	valueKey = "c"

	// For dynamodb errors
	tableNameLabel   = "table"
	errorReasonLabel = "error"
	otherError       = "other"

	// See http://docs.aws.amazon.com/amazondynamodb/latest/developerguide/Limits.html.
	dynamoDBMaxWriteBatchSize = 25
	dynamoDBMaxReadBatchSize  = 100
	validationException       = "ValidationException"
)

// DynamoDBConfig specifies config for a DynamoDB database.
type DynamoDBConfig struct {
	DynamoDB               flagext.URLValue         `yaml:"dynamodb_url"`
	APILimit               float64                  `yaml:"api_limit"`
	ThrottleLimit          float64                  `yaml:"throttle_limit"`
	Metrics                MetricsAutoScalingConfig `yaml:"metrics"`
	ChunkGangSize          int                      `yaml:"chunk_gang_size"`
	ChunkGetMaxParallelism int                      `yaml:"chunk_get_max_parallelism"`
	BackoffConfig          backoff.Config           `yaml:"backoff_config"`
	KMSKeyID               string                   `yaml:"kms_key_id"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *DynamoDBConfig) RegisterFlags(f *flag.FlagSet) {
	f.Var(&cfg.DynamoDB, "dynamodb.url", "DynamoDB endpoint URL with escaped Key and Secret encoded. "+
		"If only region is specified as a host, proper endpoint will be deduced. Use inmemory:///<table-name> to use a mock in-memory implementation.")
	f.Float64Var(&cfg.APILimit, "dynamodb.api-limit", 2.0, "DynamoDB table management requests per second limit.")
	f.Float64Var(&cfg.ThrottleLimit, "dynamodb.throttle-limit", 10.0, "DynamoDB rate cap to back off when throttled.")
	f.IntVar(&cfg.ChunkGangSize, "dynamodb.chunk-gang-size", 10, "Number of chunks to group together to parallelise fetches (zero to disable)")
	f.IntVar(&cfg.ChunkGetMaxParallelism, "dynamodb.chunk.get-max-parallelism", 32, "Max number of chunk-get operations to start in parallel")
	f.DurationVar(&cfg.BackoffConfig.MinBackoff, "dynamodb.min-backoff", 100*time.Millisecond, "Minimum backoff time")
	f.DurationVar(&cfg.BackoffConfig.MaxBackoff, "dynamodb.max-backoff", 50*time.Second, "Maximum backoff time")
	f.IntVar(&cfg.BackoffConfig.MaxRetries, "dynamodb.max-retries", 20, "Maximum number of times to retry an operation")
	f.StringVar(&cfg.KMSKeyID, "dynamodb.kms-key-id", "", "KMS key used for encrypting DynamoDB items.  DynamoDB will use an Amazon owned KMS key if not provided.")
	cfg.Metrics.RegisterFlags(f)
}

// StorageConfig specifies config for storing data on AWS.
type StorageConfig struct {
	DynamoDBConfig `yaml:"dynamodb" doc:"description=Deprecated: Configures storing indexes in DynamoDB."`
	S3Config       `yaml:",inline"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *StorageConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.DynamoDBConfig.RegisterFlags(f)
	cfg.S3Config.RegisterFlags(f)
}

// Validate config and returns error on failure
func (cfg *StorageConfig) Validate() error {
	if err := cfg.S3Config.Validate(); err != nil {
		return errors.Wrap(err, "invalid S3 Storage config")
	}
	return nil
}

type dynamoDBStorageClient struct {
	cfg       DynamoDBConfig
	schemaCfg config.SchemaConfig

	DynamoDB dynamodbiface.DynamoDBAPI
	// These rate-limiters let us slow down when DynamoDB signals provision limits.
	writeThrottle *rate.Limiter

	// These functions exists for mocking, so we don't have to write a whole load
	// of boilerplate.
	batchGetItemRequestFn   func(ctx context.Context, input *dynamodb.BatchGetItemInput) dynamoDBRequest
	batchWriteItemRequestFn func(ctx context.Context, input *dynamodb.BatchWriteItemInput) dynamoDBRequest

	metrics *dynamoDBMetrics
}

// NewDynamoDBIndexClient makes a new DynamoDB-backed IndexClient.
func NewDynamoDBIndexClient(cfg DynamoDBConfig, schemaCfg config.SchemaConfig, reg prometheus.Registerer) (index.Client, error) {
	return newDynamoDBStorageClient(cfg, schemaCfg, reg)
}

// NewDynamoDBChunkClient makes a new DynamoDB-backed chunk.Client.
func NewDynamoDBChunkClient(cfg DynamoDBConfig, schemaCfg config.SchemaConfig, reg prometheus.Registerer) (chunkclient.Client, error) {
	return newDynamoDBStorageClient(cfg, schemaCfg, reg)
}

// newDynamoDBStorageClient makes a new DynamoDB-backed IndexClient and chunk.Client.
func newDynamoDBStorageClient(cfg DynamoDBConfig, schemaCfg config.SchemaConfig, reg prometheus.Registerer) (*dynamoDBStorageClient, error) {
	dynamoDB, err := dynamoClientFromURL(cfg.DynamoDB.URL)
	if err != nil {
		return nil, err
	}

	client := &dynamoDBStorageClient{
		cfg:           cfg,
		schemaCfg:     schemaCfg,
		DynamoDB:      dynamoDB,
		writeThrottle: rate.NewLimiter(rate.Limit(cfg.ThrottleLimit), dynamoDBMaxWriteBatchSize),
		metrics:       newMetrics(reg),
	}
	client.batchGetItemRequestFn = client.batchGetItemRequest
	client.batchWriteItemRequestFn = client.batchWriteItemRequest
	return client, nil
}

// Stop implements chunk.IndexClient.
func (a dynamoDBStorageClient) Stop() {
}

// NewWriteBatch implements chunk.IndexClient.
func (a dynamoDBStorageClient) NewWriteBatch() index.WriteBatch {
	return dynamoDBWriteBatch(map[string][]*dynamodb.WriteRequest{})
}

func logWriteRetry(unprocessed dynamoDBWriteBatch, metrics *dynamoDBMetrics) {
	for table, reqs := range unprocessed {
		metrics.dynamoThrottled.WithLabelValues("DynamoDB.BatchWriteItem", table).Add(float64(len(reqs)))
		for _, req := range reqs {
			item := req.PutRequest.Item
			var hash, rnge string
			if hashAttr, ok := item[hashKey]; ok {
				if hashAttr.S != nil {
					hash = *hashAttr.S
				}
			}
			if rangeAttr, ok := item[rangeKey]; ok {
				rnge = string(rangeAttr.B)
			}
			util.Event().Log("msg", "store retry", "table", table, "hashKey", hash, "rangeKey", rnge)
		}
	}
}

// BatchWrite writes requests to the underlying storage, handling retries and backoff.
// Structure is identical to getDynamoDBChunks(), but operating on different datatypes
// so cannot share implementation.  If you fix a bug here fix it there too.
func (a dynamoDBStorageClient) BatchWrite(ctx context.Context, input index.WriteBatch) error {
	outstanding := input.(dynamoDBWriteBatch)
	unprocessed := dynamoDBWriteBatch{}

	backoff := backoff.New(ctx, a.cfg.BackoffConfig)

	for outstanding.Len()+unprocessed.Len() > 0 && backoff.Ongoing() {
		requests := dynamoDBWriteBatch{}
		requests.TakeReqs(outstanding, dynamoDBMaxWriteBatchSize)
		requests.TakeReqs(unprocessed, dynamoDBMaxWriteBatchSize)

		request := a.batchWriteItemRequestFn(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems:           requests,
			ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
		})

		err := instrument.CollectedRequest(ctx, "DynamoDB.BatchWriteItem", a.metrics.dynamoRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
			return request.Send()
		})
		resp := request.Data().(*dynamodb.BatchWriteItemOutput)

		for _, cc := range resp.ConsumedCapacity {
			a.metrics.dynamoConsumedCapacity.WithLabelValues("DynamoDB.BatchWriteItem", *cc.TableName).
				Add(*cc.CapacityUnits)
		}

		if err != nil {
			for tableName := range requests {
				recordDynamoError(tableName, err, "DynamoDB.BatchWriteItem", a.metrics)
			}

			// If we get provisionedThroughputExceededException, then no items were processed,
			// so back off and retry all.
			if awsErr, ok := err.(awserr.Error); ok && ((awsErr.Code() == dynamodb.ErrCodeProvisionedThroughputExceededException) || request.Retryable()) {
				logWriteRetry(requests, a.metrics)
				unprocessed.TakeReqs(requests, -1)
				_ = a.writeThrottle.WaitN(ctx, len(requests))
				backoff.Wait()
				continue
			} else if ok && awsErr.Code() == validationException {
				// this write will never work, so the only option is to drop the offending items and continue.
				level.Warn(log.Logger).Log("msg", "Data lost while flushing to DynamoDB", "err", awsErr)
				level.Debug(log.Logger).Log("msg", "Dropped request details", "requests", requests)
				util.Event().Log("msg", "ValidationException", "requests", requests)
				// recording the drop counter separately from recordDynamoError(), as the error code alone may not provide enough context
				// to determine if a request was dropped (or not)
				for tableName := range requests {
					a.metrics.dynamoDroppedRequests.WithLabelValues(tableName, validationException, "DynamoDB.BatchWriteItem").Inc()
				}
				continue
			}

			// All other errors are critical.
			return err
		}

		// If there are unprocessed items, retry those items.
		unprocessedItems := dynamoDBWriteBatch(resp.UnprocessedItems)
		if len(unprocessedItems) > 0 {
			logWriteRetry(unprocessedItems, a.metrics)
			_ = a.writeThrottle.WaitN(ctx, unprocessedItems.Len())
			unprocessed.TakeReqs(unprocessedItems, -1)
		}

		backoff.Reset()
	}

	if valuesLeft := outstanding.Len() + unprocessed.Len(); valuesLeft > 0 {
		return fmt.Errorf("failed to write items to DynamoDB, %d values remaining: %s", valuesLeft, backoff.Err())
	}
	return backoff.Err()
}

// QueryPages implements chunk.IndexClient.
func (a dynamoDBStorageClient) QueryPages(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
	return client_util.DoParallelQueries(ctx, a.query, queries, callback)
}

func (a dynamoDBStorageClient) query(ctx context.Context, query index.Query, callback index.QueryPagesCallback) error {
	input := &dynamodb.QueryInput{
		TableName: aws.String(query.TableName),
		KeyConditions: map[string]*dynamodb.Condition{
			hashKey: {
				AttributeValueList: []*dynamodb.AttributeValue{
					{S: aws.String(query.HashValue)},
				},
				ComparisonOperator: aws.String(dynamodb.ComparisonOperatorEq),
			},
		},
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}

	if query.RangeValuePrefix != nil {
		input.KeyConditions[rangeKey] = &dynamodb.Condition{
			AttributeValueList: []*dynamodb.AttributeValue{
				{B: query.RangeValuePrefix},
			},
			ComparisonOperator: aws.String(dynamodb.ComparisonOperatorBeginsWith),
		}
	} else if query.RangeValueStart != nil {
		input.KeyConditions[rangeKey] = &dynamodb.Condition{
			AttributeValueList: []*dynamodb.AttributeValue{
				{B: query.RangeValueStart},
			},
			ComparisonOperator: aws.String(dynamodb.ComparisonOperatorGe),
		}
	}

	// Filters
	if query.ValueEqual != nil {
		input.FilterExpression = aws.String(fmt.Sprintf("%s = :v", valueKey))
		input.ExpressionAttributeValues = map[string]*dynamodb.AttributeValue{
			":v": {
				B: query.ValueEqual,
			},
		}
	}

	pageCount := 0
	defer func() {
		a.metrics.dynamoQueryPagesCount.Observe(float64(pageCount))
	}()

	retryer := newRetryer(ctx, a.cfg.BackoffConfig)
	err := instrument.CollectedRequest(ctx, "DynamoDB.QueryPages", a.metrics.dynamoRequestDuration, instrument.ErrorCode, func(innerCtx context.Context) error {
		if sp := ot.SpanFromContext(innerCtx); sp != nil {
			sp.SetTag("tableName", query.TableName)
			sp.SetTag("hashValue", query.HashValue)
		}
		return a.DynamoDB.QueryPagesWithContext(innerCtx, input, func(output *dynamodb.QueryOutput, _ bool) bool {
			pageCount++
			if sp := ot.SpanFromContext(innerCtx); sp != nil {
				sp.LogFields(otlog.Int("page", pageCount))
			}

			if cc := output.ConsumedCapacity; cc != nil {
				a.metrics.dynamoConsumedCapacity.WithLabelValues("DynamoDB.QueryPages", *cc.TableName).
					Add(*cc.CapacityUnits)
			}

			return callback(query, &dynamoDBReadResponse{items: output.Items})
		}, retryer.withRetries, withErrorHandler(query.TableName, "DynamoDB.QueryPages", a.metrics))
	})
	if err != nil {
		return errors.Wrapf(err, "QueryPages error: table=%v", query.TableName)
	}
	return err
}

type dynamoDBRequest interface {
	Send() error
	Data() interface{}
	Error() error
	Retryable() bool
}

func (a dynamoDBStorageClient) batchGetItemRequest(ctx context.Context, input *dynamodb.BatchGetItemInput) dynamoDBRequest {
	req, _ := a.DynamoDB.BatchGetItemRequest(input)
	req.SetContext(ctx)
	return dynamoDBRequestAdapter{req}
}

func (a dynamoDBStorageClient) batchWriteItemRequest(ctx context.Context, input *dynamodb.BatchWriteItemInput) dynamoDBRequest {
	req, _ := a.DynamoDB.BatchWriteItemRequest(input)
	req.SetContext(ctx)
	return dynamoDBRequestAdapter{req}
}

type dynamoDBRequestAdapter struct {
	request *request.Request
}

func (a dynamoDBRequestAdapter) Data() interface{} {
	return a.request.Data
}

func (a dynamoDBRequestAdapter) Send() error {
	return a.request.Send()
}

func (a dynamoDBRequestAdapter) Error() error {
	return a.request.Error
}

func (a dynamoDBRequestAdapter) Retryable() bool {
	return aws.BoolValue(a.request.Retryable)
}

type chunksPlusError struct {
	chunks []chunk.Chunk
	err    error
}

// GetChunks implements chunk.Client.
func (a dynamoDBStorageClient) GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	log, ctx := spanlogger.New(ctx, "GetChunks.DynamoDB", ot.Tag{Key: "numChunks", Value: len(chunks)})
	defer log.Span.Finish()
	level.Debug(log).Log("chunks requested", len(chunks))

	dynamoDBChunks := chunks
	var err error

	gangSize := a.cfg.ChunkGangSize * dynamoDBMaxReadBatchSize
	if gangSize == 0 { // zero means turn feature off
		gangSize = len(dynamoDBChunks)
	} else {
		if len(dynamoDBChunks)/gangSize > a.cfg.ChunkGetMaxParallelism {
			gangSize = len(dynamoDBChunks)/a.cfg.ChunkGetMaxParallelism + 1
		}
	}

	results := make(chan chunksPlusError)
	for i := 0; i < len(dynamoDBChunks); i += gangSize {
		go func(start int) {
			end := start + gangSize
			if end > len(dynamoDBChunks) {
				end = len(dynamoDBChunks)
			}
			outChunks, err := a.getDynamoDBChunks(ctx, dynamoDBChunks[start:end])
			results <- chunksPlusError{outChunks, err}
		}(i)
	}
	finalChunks := []chunk.Chunk{}
	for i := 0; i < len(dynamoDBChunks); i += gangSize {
		in := <-results
		if in.err != nil {
			err = in.err // TODO: cancel other sub-queries at this point
		}
		finalChunks = append(finalChunks, in.chunks...)
	}
	level.Debug(log).Log("chunks fetched", len(finalChunks))

	// Return any chunks we did receive: a partial result may be useful
	return finalChunks, log.Error(err)
}

// As we're re-using the DynamoDB schema from the index for the chunk tables,
// we need to provide a non-null, non-empty value for the range value.
var placeholder = []byte{'c'}

// Fetch a set of chunks from DynamoDB, handling retries and backoff.
// Structure is identical to BatchWrite(), but operating on different datatypes
// so cannot share implementation.  If you fix a bug here fix it there too.
func (a dynamoDBStorageClient) getDynamoDBChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	log, ctx := spanlogger.New(ctx, "getDynamoDBChunks", ot.Tag{Key: "numChunks", Value: len(chunks)})
	defer log.Span.Finish()
	outstanding := dynamoDBReadRequest{}
	chunksByKey := map[string]chunk.Chunk{}
	for _, chunk := range chunks {
		key := a.schemaCfg.ExternalKey(chunk.ChunkRef)
		chunksByKey[key] = chunk
		tableName, err := a.schemaCfg.ChunkTableFor(chunk.From)
		if err != nil {
			return nil, log.Error(err)
		}
		outstanding.Add(tableName, key, placeholder)
	}

	result := []chunk.Chunk{}
	unprocessed := dynamoDBReadRequest{}
	backoff := backoff.New(ctx, a.cfg.BackoffConfig)

	for outstanding.Len()+unprocessed.Len() > 0 && backoff.Ongoing() {
		requests := dynamoDBReadRequest{}
		requests.TakeReqs(outstanding, dynamoDBMaxReadBatchSize)
		requests.TakeReqs(unprocessed, dynamoDBMaxReadBatchSize)

		request := a.batchGetItemRequestFn(ctx, &dynamodb.BatchGetItemInput{
			RequestItems:           requests,
			ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
		})

		err := instrument.CollectedRequest(ctx, "DynamoDB.BatchGetItemPages", a.metrics.dynamoRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
			return request.Send()
		})
		response := request.Data().(*dynamodb.BatchGetItemOutput)

		for _, cc := range response.ConsumedCapacity {
			a.metrics.dynamoConsumedCapacity.WithLabelValues("DynamoDB.BatchGetItemPages", *cc.TableName).
				Add(*cc.CapacityUnits)
		}

		if err != nil {
			for tableName := range requests {
				recordDynamoError(tableName, err, "DynamoDB.BatchGetItemPages", a.metrics)
			}

			// If we get provisionedThroughputExceededException, then no items were processed,
			// so back off and retry all.
			if awsErr, ok := err.(awserr.Error); ok && ((awsErr.Code() == dynamodb.ErrCodeProvisionedThroughputExceededException) || request.Retryable()) {
				unprocessed.TakeReqs(requests, -1)
				backoff.Wait()
				continue
			} else if ok && awsErr.Code() == validationException {
				// this read will never work, so the only option is to drop the offending request and continue.
				level.Warn(log).Log("msg", "Error while fetching data from Dynamo", "err", awsErr)
				level.Debug(log).Log("msg", "Dropped request details", "requests", requests)
				// recording the drop counter separately from recordDynamoError(), as the error code alone may not provide enough context
				// to determine if a request was dropped (or not)
				for tableName := range requests {
					a.metrics.dynamoDroppedRequests.WithLabelValues(tableName, validationException, "DynamoDB.BatchGetItemPages").Inc()
				}
				continue
			}

			// All other errors are critical.
			return nil, err
		}

		processedChunks, err := processChunkResponse(response, chunksByKey)
		if err != nil {
			return nil, log.Error(err)
		}
		result = append(result, processedChunks...)

		// If there are unprocessed items, retry those items.
		if unprocessedKeys := response.UnprocessedKeys; unprocessedKeys != nil && dynamoDBReadRequest(unprocessedKeys).Len() > 0 {
			unprocessed.TakeReqs(unprocessedKeys, -1)
		}

		backoff.Reset()
	}

	if valuesLeft := outstanding.Len() + unprocessed.Len(); valuesLeft > 0 {
		// Return the chunks we did fetch, because partial results may be useful
		return result, log.Error(fmt.Errorf("failed to query chunks, %d values remaining: %s", valuesLeft, backoff.Err()))
	}
	return result, nil
}

func processChunkResponse(response *dynamodb.BatchGetItemOutput, chunksByKey map[string]chunk.Chunk) ([]chunk.Chunk, error) {
	result := []chunk.Chunk{}
	decodeContext := chunk.NewDecodeContext()
	for _, items := range response.Responses {
		for _, item := range items {
			key, ok := item[hashKey]
			if !ok || key == nil || key.S == nil {
				return nil, fmt.Errorf("Got response from DynamoDB with no hash key: %+v", item)
			}

			chunk, ok := chunksByKey[*key.S]
			if !ok {
				return nil, fmt.Errorf("Got response from DynamoDB with chunk I didn't ask for: %s", *key.S)
			}

			buf, ok := item[valueKey]
			if !ok || buf == nil || buf.B == nil {
				return nil, fmt.Errorf("Got response from DynamoDB with no value: %+v", item)
			}

			if err := chunk.Decode(decodeContext, buf.B); err != nil {
				return nil, err
			}

			result = append(result, chunk)
		}
	}
	return result, nil
}

// PutChunksAndIndex implements chunk.ObjectAndIndexClient
// Combine both sets of writes before sending to DynamoDB, for performance
func (a dynamoDBStorageClient) PutChunksAndIndex(ctx context.Context, chunks []chunk.Chunk, index index.WriteBatch) error {
	dynamoDBWrites, err := a.writesForChunks(chunks)
	if err != nil {
		return err
	}
	dynamoDBWrites.TakeReqs(index.(dynamoDBWriteBatch), 0)
	return a.BatchWrite(ctx, dynamoDBWrites)
}

// PutChunks implements chunk.Client.
func (a dynamoDBStorageClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	dynamoDBWrites, err := a.writesForChunks(chunks)
	if err != nil {
		return err
	}
	return a.BatchWrite(ctx, dynamoDBWrites)
}

func (a dynamoDBStorageClient) DeleteChunk(ctx context.Context, userID, chunkID string) error {
	chunkRef, err := chunk.ParseExternalKey(userID, chunkID)
	if err != nil {
		return err
	}

	tableName, err := a.schemaCfg.ChunkTableFor(chunkRef.From)
	if err != nil {
		return err
	}

	dynamoDBWrites := dynamoDBWriteBatch{}
	dynamoDBWrites.Delete(tableName, chunkID, placeholder)
	return a.BatchWrite(ctx, dynamoDBWrites)
}

func (a dynamoDBStorageClient) IsChunkNotFoundErr(_ error) bool {
	return false
}

func (a dynamoDBStorageClient) IsRetryableErr(_ error) bool {
	return false
}

func (a dynamoDBStorageClient) writesForChunks(chunks []chunk.Chunk) (dynamoDBWriteBatch, error) {
	dynamoDBWrites := dynamoDBWriteBatch{}

	for i := range chunks {
		buf, err := chunks[i].Encoded()
		if err != nil {
			return nil, err
		}
		key := a.schemaCfg.ExternalKey(chunks[i].ChunkRef)

		table, err := a.schemaCfg.ChunkTableFor(chunks[i].From)
		if err != nil {
			return nil, err
		}

		dynamoDBWrites.Add(table, key, placeholder, buf)
	}

	return dynamoDBWrites, nil
}

// Slice of values returned; map key is attribute name
type dynamoDBReadResponse struct {
	items []map[string]*dynamodb.AttributeValue
}

func (b *dynamoDBReadResponse) Iterator() index.ReadBatchIterator {
	return &dynamoDBReadResponseIterator{
		i:                    -1,
		dynamoDBReadResponse: b,
	}
}

type dynamoDBReadResponseIterator struct {
	i int
	*dynamoDBReadResponse
}

func (b *dynamoDBReadResponseIterator) Next() bool {
	b.i++
	return b.i < len(b.items)
}

func (b *dynamoDBReadResponseIterator) RangeValue() []byte {
	return b.items[b.i][rangeKey].B
}

func (b *dynamoDBReadResponseIterator) Value() []byte {
	chunkValue, ok := b.items[b.i][valueKey]
	if !ok {
		return nil
	}
	return chunkValue.B
}

// map key is table name; value is a slice of things to 'put'
type dynamoDBWriteBatch map[string][]*dynamodb.WriteRequest

func (b dynamoDBWriteBatch) Len() int {
	result := 0
	for _, reqs := range b {
		result += len(reqs)
	}
	return result
}

func (b dynamoDBWriteBatch) String() string {
	var sb strings.Builder
	sb.WriteByte('{')
	for k, reqs := range b {
		sb.WriteString(k)
		sb.WriteString(": [")
		for _, req := range reqs {
			sb.WriteString(req.String())
			sb.WriteByte(',')
		}
		sb.WriteString("], ")
	}
	sb.WriteByte('}')
	return sb.String()
}

func (b dynamoDBWriteBatch) Add(tableName, hashValue string, rangeValue []byte, value []byte) {
	item := map[string]*dynamodb.AttributeValue{
		hashKey:  {S: aws.String(hashValue)},
		rangeKey: {B: rangeValue},
	}

	if value != nil {
		item[valueKey] = &dynamodb.AttributeValue{B: value}
	}

	b[tableName] = append(b[tableName], &dynamodb.WriteRequest{
		PutRequest: &dynamodb.PutRequest{
			Item: item,
		},
	})
}

func (b dynamoDBWriteBatch) Delete(tableName, hashValue string, rangeValue []byte) {
	b[tableName] = append(b[tableName], &dynamodb.WriteRequest{
		DeleteRequest: &dynamodb.DeleteRequest{
			Key: map[string]*dynamodb.AttributeValue{
				hashKey:  {S: aws.String(hashValue)},
				rangeKey: {B: rangeValue},
			},
		},
	})
}

// Fill 'b' with WriteRequests from 'from' until 'b' has at most max requests. Remove those requests from 'from'.
func (b dynamoDBWriteBatch) TakeReqs(from dynamoDBWriteBatch, max int) {
	outLen, inLen := b.Len(), from.Len()
	toFill := inLen
	if max > 0 {
		toFill = math.Min(inLen, max-outLen)
	}
	for toFill > 0 {
		for tableName, fromReqs := range from {
			taken := math.Min(len(fromReqs), toFill)
			if taken > 0 {
				b[tableName] = append(b[tableName], fromReqs[:taken]...)
				from[tableName] = fromReqs[taken:]
				toFill -= taken
			}
		}
	}
}

// map key is table name
type dynamoDBReadRequest map[string]*dynamodb.KeysAndAttributes

func (b dynamoDBReadRequest) Len() int {
	result := 0
	for _, reqs := range b {
		result += len(reqs.Keys)
	}
	return result
}

func (b dynamoDBReadRequest) Add(tableName, hashValue string, rangeValue []byte) {
	requests, ok := b[tableName]
	if !ok {
		requests = &dynamodb.KeysAndAttributes{
			AttributesToGet: []*string{
				aws.String(hashKey),
				aws.String(valueKey),
			},
		}
		b[tableName] = requests
	}
	requests.Keys = append(requests.Keys, map[string]*dynamodb.AttributeValue{
		hashKey:  {S: aws.String(hashValue)},
		rangeKey: {B: rangeValue},
	})
}

// Fill 'b' with ReadRequests from 'from' until 'b' has at most max requests. Remove those requests from 'from'.
func (b dynamoDBReadRequest) TakeReqs(from dynamoDBReadRequest, max int) {
	outLen, inLen := b.Len(), from.Len()
	toFill := inLen
	if max > 0 {
		toFill = math.Min(inLen, max-outLen)
	}
	for toFill > 0 {
		for tableName, fromReqs := range from {
			taken := math.Min(len(fromReqs.Keys), toFill)
			if taken > 0 {
				if _, ok := b[tableName]; !ok {
					b[tableName] = &dynamodb.KeysAndAttributes{
						AttributesToGet: []*string{
							aws.String(hashKey),
							aws.String(valueKey),
						},
					}
				}

				b[tableName].Keys = append(b[tableName].Keys, fromReqs.Keys[:taken]...)
				from[tableName].Keys = fromReqs.Keys[taken:]
				toFill -= taken
			}
		}
	}
}

func withErrorHandler(tableName, operation string, metrics *dynamoDBMetrics) func(req *request.Request) {
	return func(req *request.Request) {
		req.Handlers.CompleteAttempt.PushBack(func(req *request.Request) {
			if req.Error != nil {
				recordDynamoError(tableName, req.Error, operation, metrics)
			}
		})
	}
}

func recordDynamoError(tableName string, err error, operation string, metrics *dynamoDBMetrics) {
	if awsErr, ok := err.(awserr.Error); ok {
		metrics.dynamoFailures.WithLabelValues(tableName, awsErr.Code(), operation).Add(float64(1))
	} else {
		metrics.dynamoFailures.WithLabelValues(tableName, otherError, operation).Add(float64(1))
	}
}

// dynamoClientFromURL creates a new DynamoDB client from a URL.
func dynamoClientFromURL(awsURL *url.URL) (dynamodbiface.DynamoDBAPI, error) {
	dynamoDBSession, err := awsSessionFromURL(awsURL)
	if err != nil {
		return nil, err
	}
	return dynamodb.New(dynamoDBSession), nil
}

// awsSessionFromURL creates a new aws session from a URL.
func awsSessionFromURL(awsURL *url.URL) (client.ConfigProvider, error) {
	if awsURL == nil {
		return nil, fmt.Errorf("no URL specified for DynamoDB")
	}
	path := strings.TrimPrefix(awsURL.Path, "/")
	if len(path) > 0 {
		level.Warn(log.Logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
	}
	config, err := awscommon.ConfigFromURL(awsURL)
	if err != nil {
		return nil, err
	}
	config = config.WithMaxRetries(0) // We do our own retries, so we can monitor them
	config = config.WithHTTPClient(&http.Client{Transport: defaultTransport})
	return session.NewSession(config)
}

// Copy-pasted http.DefaultTransport
var defaultTransport http.RoundTripper = &http.Transport{
	Proxy: http.ProxyFromEnvironment,
	DialContext: (&net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
	}).DialContext,
	ForceAttemptHTTP2: true,
	MaxIdleConns:      100,
	// We will connect many times in parallel to the same DynamoDB server,
	// see https://github.com/golang/go/issues/13801
	MaxIdleConnsPerHost:   100,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   10 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
}

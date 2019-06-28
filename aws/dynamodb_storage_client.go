package aws

import (
	"context"
	"flag"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-kit/kit/log/level"
	ot "github.com/opentracing/opentracing-go"
	"golang.org/x/time/rate"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/chunk"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/cortexproject/cortex/pkg/util/spanlogger"
	awscommon "github.com/weaveworks/common/aws"
	"github.com/weaveworks/common/instrument"
	"github.com/weaveworks/common/user"
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

var (
	dynamoRequestDuration = instrument.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "dynamo_request_duration_seconds",
		Help:      "Time spent doing DynamoDB requests.",

		// DynamoDB latency seems to range from a few ms to a few sec and is
		// important.  So use 8 buckets from 128us to 2s.
		Buckets: prometheus.ExponentialBuckets(0.000128, 4, 8),
	}, []string{"operation", "status_code"}))
	dynamoConsumedCapacity = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "dynamo_consumed_capacity_total",
		Help:      "The capacity units consumed by operation.",
	}, []string{"operation", tableNameLabel})
	dynamoThrottled = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "dynamo_throttled_total",
		Help:      "The total number of throttled events.",
	}, []string{"operation", tableNameLabel})
	dynamoFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "dynamo_failures_total",
		Help:      "The total number of errors while storing chunks to the chunk store.",
	}, []string{tableNameLabel, errorReasonLabel, "operation"})
	dynamoDroppedRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "dynamo_dropped_requests_total",
		Help:      "The total number of requests which were dropped due to errors encountered from dynamo.",
	}, []string{tableNameLabel, errorReasonLabel, "operation"})
	dynamoQueryPagesCount = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "dynamo_query_pages_count",
		Help:      "Number of pages per query.",
		// Most queries will have one page, however this may increase with fuzzy
		// metric names.
		Buckets: prometheus.ExponentialBuckets(1, 4, 6),
	})
)

func init() {
	dynamoRequestDuration.Register()
	prometheus.MustRegister(dynamoConsumedCapacity)
	prometheus.MustRegister(dynamoThrottled)
	prometheus.MustRegister(dynamoFailures)
	prometheus.MustRegister(dynamoQueryPagesCount)
	prometheus.MustRegister(dynamoDroppedRequests)
}

// DynamoDBConfig specifies config for a DynamoDB database.
type DynamoDBConfig struct {
	DynamoDB               flagext.URLValue
	APILimit               float64
	ThrottleLimit          float64
	ApplicationAutoScaling flagext.URLValue
	Metrics                MetricsAutoScalingConfig
	ChunkGangSize          int
	ChunkGetMaxParallelism int
	backoffConfig          util.BackoffConfig
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *DynamoDBConfig) RegisterFlags(f *flag.FlagSet) {
	f.Var(&cfg.DynamoDB, "dynamodb.url", "DynamoDB endpoint URL with escaped Key and Secret encoded. "+
		"If only region is specified as a host, proper endpoint will be deduced. Use inmemory:///<table-name> to use a mock in-memory implementation.")
	f.Float64Var(&cfg.APILimit, "dynamodb.api-limit", 2.0, "DynamoDB table management requests per second limit.")
	f.Float64Var(&cfg.ThrottleLimit, "dynamodb.throttle-limit", 10.0, "DynamoDB rate cap to back off when throttled.")
	f.Var(&cfg.ApplicationAutoScaling, "applicationautoscaling.url", "ApplicationAutoscaling endpoint URL with escaped Key and Secret encoded.")
	f.IntVar(&cfg.ChunkGangSize, "dynamodb.chunk.gang.size", 10, "Number of chunks to group together to parallelise fetches (zero to disable)")
	f.IntVar(&cfg.ChunkGetMaxParallelism, "dynamodb.chunk.get.max.parallelism", 32, "Max number of chunk-get operations to start in parallel")
	f.DurationVar(&cfg.backoffConfig.MinBackoff, "dynamodb.min-backoff", 100*time.Millisecond, "Minimum backoff time")
	f.DurationVar(&cfg.backoffConfig.MaxBackoff, "dynamodb.max-backoff", 50*time.Second, "Maximum backoff time")
	f.IntVar(&cfg.backoffConfig.MaxRetries, "dynamodb.max-retries", 20, "Maximum number of times to retry an operation")
	cfg.Metrics.RegisterFlags(f)
}

// StorageConfig specifies config for storing data on AWS.
type StorageConfig struct {
	DynamoDBConfig
	S3               flagext.URLValue
	S3ForcePathStyle bool
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *StorageConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.DynamoDBConfig.RegisterFlags(f)

	f.Var(&cfg.S3, "s3.url", "S3 endpoint URL with escaped Key and Secret encoded. "+
		"If only region is specified as a host, proper endpoint will be deduced. Use inmemory:///<bucket-name> to use a mock in-memory implementation.")
	f.BoolVar(&cfg.S3ForcePathStyle, "s3.force-path-style", false, "Set this to `true` to force the request to use path-style addressing.")
}

type dynamoDBStorageClient struct {
	cfg       DynamoDBConfig
	schemaCfg chunk.SchemaConfig

	DynamoDB dynamodbiface.DynamoDBAPI
	// These rate-limiters let us slow down when DynamoDB signals provision limits.
	writeThrottle *rate.Limiter

	// These functions exists for mocking, so we don't have to write a whole load
	// of boilerplate.
	queryRequestFn          func(ctx context.Context, input *dynamodb.QueryInput) dynamoDBRequest
	batchGetItemRequestFn   func(ctx context.Context, input *dynamodb.BatchGetItemInput) dynamoDBRequest
	batchWriteItemRequestFn func(ctx context.Context, input *dynamodb.BatchWriteItemInput) dynamoDBRequest
}

// NewDynamoDBIndexClient makes a new DynamoDB-backed IndexClient.
func NewDynamoDBIndexClient(cfg DynamoDBConfig, schemaCfg chunk.SchemaConfig) (chunk.IndexClient, error) {
	return newDynamoDBStorageClient(cfg, schemaCfg)
}

// NewDynamoDBObjectClient makes a new DynamoDB-backed ObjectClient.
func NewDynamoDBObjectClient(cfg DynamoDBConfig, schemaCfg chunk.SchemaConfig) (chunk.ObjectClient, error) {
	return newDynamoDBStorageClient(cfg, schemaCfg)
}

// newDynamoDBStorageClient makes a new DynamoDB-backed IndexClient and ObjectClient.
func newDynamoDBStorageClient(cfg DynamoDBConfig, schemaCfg chunk.SchemaConfig) (*dynamoDBStorageClient, error) {
	dynamoDB, err := dynamoClientFromURL(cfg.DynamoDB.URL)
	if err != nil {
		return nil, err
	}

	client := &dynamoDBStorageClient{
		cfg:           cfg,
		schemaCfg:     schemaCfg,
		DynamoDB:      dynamoDB,
		writeThrottle: rate.NewLimiter(rate.Limit(cfg.ThrottleLimit), dynamoDBMaxWriteBatchSize),
	}
	client.queryRequestFn = client.queryRequest
	client.batchGetItemRequestFn = client.batchGetItemRequest
	client.batchWriteItemRequestFn = client.batchWriteItemRequest
	return client, nil
}

// Stop implements chunk.IndexClient.
func (a dynamoDBStorageClient) Stop() {
}

// NewWriteBatch implements chunk.IndexClient.
func (a dynamoDBStorageClient) NewWriteBatch() chunk.WriteBatch {
	return dynamoDBWriteBatch(map[string][]*dynamodb.WriteRequest{})
}

func logWriteRetry(ctx context.Context, unprocessed dynamoDBWriteBatch) {
	userID, _ := user.ExtractOrgID(ctx)
	for table, reqs := range unprocessed {
		dynamoThrottled.WithLabelValues("DynamoDB.BatchWriteItem", table).Add(float64(len(reqs)))
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
			util.Event().Log("msg", "store retry", "table", table, "userID", userID, "hashKey", hash, "rangeKey", rnge)
		}
	}
}

// BatchWrite writes requests to the underlying storage, handling retries and backoff.
// Structure is identical to getDynamoDBChunks(), but operating on different datatypes
// so cannot share implementation.  If you fix a bug here fix it there too.
func (a dynamoDBStorageClient) BatchWrite(ctx context.Context, input chunk.WriteBatch) error {
	outstanding := input.(dynamoDBWriteBatch)
	unprocessed := dynamoDBWriteBatch{}

	backoff := util.NewBackoff(ctx, a.cfg.backoffConfig)

	for outstanding.Len()+unprocessed.Len() > 0 && backoff.Ongoing() {
		requests := dynamoDBWriteBatch{}
		requests.TakeReqs(outstanding, dynamoDBMaxWriteBatchSize)
		requests.TakeReqs(unprocessed, dynamoDBMaxWriteBatchSize)

		request := a.batchWriteItemRequestFn(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems:           requests,
			ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
		})

		err := instrument.CollectedRequest(ctx, "DynamoDB.BatchWriteItem", dynamoRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			return request.Send()
		})
		resp := request.Data().(*dynamodb.BatchWriteItemOutput)

		for _, cc := range resp.ConsumedCapacity {
			dynamoConsumedCapacity.WithLabelValues("DynamoDB.BatchWriteItem", *cc.TableName).
				Add(float64(*cc.CapacityUnits))
		}

		if err != nil {
			for tableName := range requests {
				recordDynamoError(tableName, err, "DynamoDB.BatchWriteItem")
			}

			// If we get provisionedThroughputExceededException, then no items were processed,
			// so back off and retry all.
			if awsErr, ok := err.(awserr.Error); ok && ((awsErr.Code() == dynamodb.ErrCodeProvisionedThroughputExceededException) || request.Retryable()) {
				logWriteRetry(ctx, requests)
				unprocessed.TakeReqs(requests, -1)
				a.writeThrottle.WaitN(ctx, len(requests))
				backoff.Wait()
				continue
			} else if ok && awsErr.Code() == validationException {
				// this write will never work, so the only option is to drop the offending items and continue.
				level.Warn(util.Logger).Log("msg", "Data lost while flushing to Dynamo", "err", awsErr)
				level.Debug(util.Logger).Log("msg", "Dropped request details", "requests", requests)
				util.Event().Log("msg", "ValidationException", "requests", requests)
				// recording the drop counter separately from recordDynamoError(), as the error code alone may not provide enough context
				// to determine if a request was dropped (or not)
				for tableName := range requests {
					dynamoDroppedRequests.WithLabelValues(tableName, validationException, "DynamoDB.BatchWriteItem").Inc()
				}
				continue
			}

			// All other errors are critical.
			return err
		}

		// If there are unprocessed items, retry those items.
		unprocessedItems := dynamoDBWriteBatch(resp.UnprocessedItems)
		if len(unprocessedItems) > 0 {
			logWriteRetry(ctx, unprocessedItems)
			a.writeThrottle.WaitN(ctx, unprocessedItems.Len())
			unprocessed.TakeReqs(unprocessedItems, -1)
		}

		backoff.Reset()
	}

	if valuesLeft := outstanding.Len() + unprocessed.Len(); valuesLeft > 0 {
		return fmt.Errorf("failed to write chunk, %d values remaining: %s", valuesLeft, backoff.Err())
	}
	return backoff.Err()
}

// QueryPages implements chunk.IndexClient.
func (a dynamoDBStorageClient) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) bool) error {
	return chunk_util.DoParallelQueries(ctx, a.query, queries, callback)
}

func (a dynamoDBStorageClient) query(ctx context.Context, query chunk.IndexQuery, callback func(result chunk.ReadBatch) (shouldContinue bool)) error {
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

	request := a.queryRequestFn(ctx, input)
	pageCount := 0
	defer func() {
		dynamoQueryPagesCount.Observe(float64(pageCount))
	}()

	for page := request; page != nil; page = page.NextPage() {
		pageCount++

		response, err := a.queryPage(ctx, input, page, query.HashValue, pageCount)
		if err != nil {
			return err
		}

		if !callback(response) {
			if err != nil {
				return fmt.Errorf("QueryPages error: table=%v, err=%v", *input.TableName, page.Error())
			}
			return nil
		}
		if !page.HasNextPage() {
			return nil
		}
	}
	return nil
}

func (a dynamoDBStorageClient) queryPage(ctx context.Context, input *dynamodb.QueryInput, page dynamoDBRequest, hashValue string, pageCount int) (*dynamoDBReadResponse, error) {
	backoff := util.NewBackoff(ctx, a.cfg.backoffConfig)

	var err error
	for backoff.Ongoing() {
		err = instrument.CollectedRequest(ctx, "DynamoDB.QueryPages", dynamoRequestDuration, instrument.ErrorCode, func(innerCtx context.Context) error {
			if sp := ot.SpanFromContext(innerCtx); sp != nil {
				sp.SetTag("tableName", aws.StringValue(input.TableName))
				sp.SetTag("hashValue", hashValue)
				sp.SetTag("page", pageCount)
				sp.SetTag("retry", backoff.NumRetries())
			}
			return page.Send()
		})

		if cc := page.Data().(*dynamodb.QueryOutput).ConsumedCapacity; cc != nil {
			dynamoConsumedCapacity.WithLabelValues("DynamoDB.QueryPages", *cc.TableName).
				Add(float64(*cc.CapacityUnits))
		}

		if err != nil {
			recordDynamoError(*input.TableName, err, "DynamoDB.QueryPages")
			if awsErr, ok := err.(awserr.Error); ok && ((awsErr.Code() == dynamodb.ErrCodeProvisionedThroughputExceededException) || page.Retryable()) {
				if awsErr.Code() != dynamodb.ErrCodeProvisionedThroughputExceededException {
					level.Warn(util.Logger).Log("msg", "DynamoDB error", "retry", backoff.NumRetries(), "table", *input.TableName, "err", err)
				}
				backoff.Wait()
				continue
			}
			return nil, fmt.Errorf("QueryPage error: table=%v, err=%v", *input.TableName, err)
		}

		queryOutput := page.Data().(*dynamodb.QueryOutput)
		return &dynamoDBReadResponse{
			items: queryOutput.Items,
		}, nil
	}
	return nil, fmt.Errorf("QueryPage error: %s for table %v, last error %v", backoff.Err(), *input.TableName, err)
}

type dynamoDBRequest interface {
	NextPage() dynamoDBRequest
	Send() error
	Data() interface{}
	Error() error
	HasNextPage() bool
	Retryable() bool
}

func (a dynamoDBStorageClient) queryRequest(ctx context.Context, input *dynamodb.QueryInput) dynamoDBRequest {
	req, _ := a.DynamoDB.QueryRequest(input)
	req.SetContext(ctx)
	return dynamoDBRequestAdapter{req}
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

func (a dynamoDBRequestAdapter) NextPage() dynamoDBRequest {
	next := a.request.NextPage()
	if next == nil {
		return nil
	}
	return dynamoDBRequestAdapter{next}
}

func (a dynamoDBRequestAdapter) Data() interface{} {
	return a.request.Data
}

func (a dynamoDBRequestAdapter) Send() error {
	// Clear error in case we are retrying the same operation - if we
	// don't do this then the same error will come back again immediately
	a.request.Error = nil
	return a.request.Send()
}

func (a dynamoDBRequestAdapter) Error() error {
	return a.request.Error
}

func (a dynamoDBRequestAdapter) HasNextPage() bool {
	return a.request.HasNextPage()
}

func (a dynamoDBRequestAdapter) Retryable() bool {
	return aws.BoolValue(a.request.Retryable)
}

type chunksPlusError struct {
	chunks []chunk.Chunk
	err    error
}

// GetChunks implements chunk.ObjectClient.
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
		key := chunk.ExternalKey()
		chunksByKey[key] = chunk
		tableName, err := a.schemaCfg.ChunkTableFor(chunk.From)
		if err != nil {
			return nil, log.Error(err)
		}
		outstanding.Add(tableName, key, placeholder)
	}

	result := []chunk.Chunk{}
	unprocessed := dynamoDBReadRequest{}
	backoff := util.NewBackoff(ctx, a.cfg.backoffConfig)

	for outstanding.Len()+unprocessed.Len() > 0 && backoff.Ongoing() {
		requests := dynamoDBReadRequest{}
		requests.TakeReqs(outstanding, dynamoDBMaxReadBatchSize)
		requests.TakeReqs(unprocessed, dynamoDBMaxReadBatchSize)

		request := a.batchGetItemRequestFn(ctx, &dynamodb.BatchGetItemInput{
			RequestItems:           requests,
			ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
		})

		err := instrument.CollectedRequest(ctx, "DynamoDB.BatchGetItemPages", dynamoRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			return request.Send()
		})
		response := request.Data().(*dynamodb.BatchGetItemOutput)

		for _, cc := range response.ConsumedCapacity {
			dynamoConsumedCapacity.WithLabelValues("DynamoDB.BatchGetItemPages", *cc.TableName).
				Add(float64(*cc.CapacityUnits))
		}

		if err != nil {
			for tableName := range requests {
				recordDynamoError(tableName, err, "DynamoDB.BatchGetItemPages")
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
					dynamoDroppedRequests.WithLabelValues(tableName, validationException, "DynamoDB.BatchGetItemPages").Inc()
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

// PutChunkAndIndex implements chunk.ObjectAndIndexClient
// Combine both sets of writes before sending to DynamoDB, for performance
func (a dynamoDBStorageClient) PutChunkAndIndex(ctx context.Context, c chunk.Chunk, index chunk.WriteBatch) error {
	dynamoDBWrites, err := a.writesForChunks([]chunk.Chunk{c})
	if err != nil {
		return err
	}
	dynamoDBWrites.TakeReqs(index.(dynamoDBWriteBatch), 0)
	return a.BatchWrite(ctx, dynamoDBWrites)
}

// PutChunks implements chunk.ObjectClient.
func (a dynamoDBStorageClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	dynamoDBWrites, err := a.writesForChunks(chunks)
	if err != nil {
		return err
	}
	return a.BatchWrite(ctx, dynamoDBWrites)
}

func (a dynamoDBStorageClient) writesForChunks(chunks []chunk.Chunk) (dynamoDBWriteBatch, error) {
	var (
		dynamoDBWrites = dynamoDBWriteBatch{}
	)

	for i := range chunks {
		buf, err := chunks[i].Encoded()
		if err != nil {
			return nil, err
		}
		key := chunks[i].ExternalKey()

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

func (b *dynamoDBReadResponse) Iterator() chunk.ReadBatchIterator {
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

// Fill 'b' with WriteRequests from 'from' until 'b' has at most max requests. Remove those requests from 'from'.
func (b dynamoDBWriteBatch) TakeReqs(from dynamoDBWriteBatch, max int) {
	outLen, inLen := b.Len(), from.Len()
	toFill := inLen
	if max > 0 {
		toFill = util.Min(inLen, max-outLen)
	}
	for toFill > 0 {
		for tableName, fromReqs := range from {
			taken := util.Min(len(fromReqs), toFill)
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
		toFill = util.Min(inLen, max-outLen)
	}
	for toFill > 0 {
		for tableName, fromReqs := range from {
			taken := util.Min(len(fromReqs.Keys), toFill)
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

func recordDynamoError(tableName string, err error, operation string) {
	if awsErr, ok := err.(awserr.Error); ok {
		dynamoFailures.WithLabelValues(tableName, awsErr.Code(), operation).Add(float64(1))
	} else {
		dynamoFailures.WithLabelValues(tableName, otherError, operation).Add(float64(1))
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
		level.Warn(util.Logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
	}
	config, err := awscommon.ConfigFromURL(awsURL)
	if err != nil {
		return nil, err
	}
	config = config.WithMaxRetries(0) // We do our own retries, so we can monitor them
	return session.New(config), nil
}

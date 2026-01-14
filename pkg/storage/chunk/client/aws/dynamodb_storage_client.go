package aws

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strings"
	"time"

	"github.com/IBM/ibm-cos-sdk-go/aws"
	"github.com/IBM/ibm-cos-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/smithy-go"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/instrument"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	attribute "go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/time/rate"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	chunkclient "github.com/grafana/loki/v3/pkg/storage/chunk/client"
	client_util "github.com/grafana/loki/v3/pkg/storage/chunk/client/util"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	"github.com/grafana/loki/v3/pkg/util"
	"github.com/grafana/loki/v3/pkg/util/log"
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

type dynamoCreator interface {
	CreateTable(ctx context.Context, params *dynamodb.CreateTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.CreateTableOutput, error)
}

type dynamoTagger interface {
	TagResource(ctx context.Context, params *dynamodb.TagResourceInput, optFns ...func(*dynamodb.Options)) (*dynamodb.TagResourceOutput, error)
}

type dynamoDeleter interface {
	DeleteTable(ctx context.Context, params *dynamodb.DeleteTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.DeleteTableOutput, error)
}

type dynamoTagLister interface {
	ListTagsOfResource(ctx context.Context, params *dynamodb.ListTagsOfResourceInput, optFns ...func(*dynamodb.Options)) (*dynamodb.ListTagsOfResourceOutput, error)
}

type dynamoTableUpdater interface {
	UpdateTable(ctx context.Context, params *dynamodb.UpdateTableInput, optFns ...func(*dynamodb.Options)) (*dynamodb.UpdateTableOutput, error)
}

type dynamoBatchItemGetter interface {
	BatchGetItem(ctx context.Context, params *dynamodb.BatchGetItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchGetItemOutput, error)
}

type dynamoBatchItemWriter interface {
	BatchWriteItem(ctx context.Context, params *dynamodb.BatchWriteItemInput, optFns ...func(*dynamodb.Options)) (*dynamodb.BatchWriteItemOutput, error)
}

type dynamoClient interface {
	dynamodb.ListTablesAPIClient
	dynamoCreator
	dynamoTagger
	dynamoDeleter
	dynamodb.DescribeTableAPIClient
	dynamoTagLister
	dynamoTableUpdater
	dynamodb.ScanAPIClient
	dynamodb.QueryAPIClient
	dynamoBatchItemGetter
	dynamoBatchItemWriter
}

type dynamoDBStorageClient struct {
	cfg       DynamoDBConfig
	schemaCfg config.SchemaConfig

	DynamoDB dynamoClient
	// These rate-limiters let us slow down when DynamoDB signals provision limits.
	writeThrottle *rate.Limiter

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
		DynamoDB:      &dynamoDB,
		writeThrottle: rate.NewLimiter(rate.Limit(cfg.ThrottleLimit), dynamoDBMaxWriteBatchSize),
		metrics:       newMetrics(reg),
	}
	return client, nil
}

// Stop implements chunk.IndexClient.
func (a dynamoDBStorageClient) Stop() {
}

// NewWriteBatch implements chunk.IndexClient.
func (a dynamoDBStorageClient) NewWriteBatch() index.WriteBatch {
	return dynamoDBWriteBatch(map[string][]types.WriteRequest{})
}

func logWriteRetry(unprocessed dynamoDBWriteBatch, metrics *dynamoDBMetrics) {
	for table, reqs := range unprocessed {
		metrics.dynamoThrottled.WithLabelValues("DynamoDB.BatchWriteItem", table).Add(float64(len(reqs)))
		for _, req := range reqs {
			item := req.PutRequest.Item
			var hash, rnge string
			if hashAttr, ok := item[hashKey]; ok {
				if hashAttr.(*types.AttributeValueMemberS).Value != "" {
					hash = hashAttr.(*types.AttributeValueMemberS).Value
				}
			}
			if rangeAttr, ok := item[rangeKey]; ok {
				rnge = string(rangeAttr.(*types.AttributeValueMemberB).Value)
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

		request := dynamodb.BatchWriteItemInput{
			RequestItems:           requests,
			ReturnConsumedCapacity: types.ReturnConsumedCapacityTotal,
		}

		var resp *dynamodb.BatchWriteItemOutput
		err := instrument.CollectedRequest(ctx, "DynamoDB.BatchWriteItem", a.metrics.dynamoRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			var requestErr error
			resp, requestErr = a.DynamoDB.BatchWriteItem(ctx, &request)
			return requestErr
		})
		if resp != nil {
			for _, cc := range resp.ConsumedCapacity {
				a.metrics.dynamoConsumedCapacity.WithLabelValues("DynamoDB.BatchWriteItem", *cc.TableName).
					Add(*cc.CapacityUnits)
			}
		}

		if err != nil {
			for tableName := range requests {
				recordDynamoError(tableName, err, "DynamoDB.BatchWriteItem", a.metrics)
			}

			// If we get provisionedThroughputExceededException, then no items were processed,
			// so back off and retry all.
			var err2 *types.ProvisionedThroughputExceededException
			var err3 smithy.APIError
			if errors.As(err, &err2) {
				logWriteRetry(requests, a.metrics)
				unprocessed.TakeReqs(requests, -1)
				_ = a.writeThrottle.WaitN(ctx, len(requests))
				backoff.Wait()
				continue
			} else if errors.As(err, &err3) && err3.ErrorCode() == "ValidationError" {
				// this write will never work, so the only option is to drop the offending items and continue.
				level.Warn(log.Logger).Log("msg", "Data lost while flushing to DynamoDB", "err", err3)
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
		KeyConditions: map[string]types.Condition{
			hashKey: {
				AttributeValueList: []types.AttributeValue{
					&types.AttributeValueMemberS{Value: query.HashValue},
				},
				ComparisonOperator: types.ComparisonOperatorEq,
			},
		},
		ReturnConsumedCapacity: types.ReturnConsumedCapacityTotal,
	}

	if query.RangeValuePrefix != nil {
		input.KeyConditions[rangeKey] = types.Condition{
			AttributeValueList: []types.AttributeValue{
				&types.AttributeValueMemberB{Value: query.RangeValuePrefix},
			},
			ComparisonOperator: types.ComparisonOperatorBeginsWith,
		}
	} else if query.RangeValueStart != nil {
		input.KeyConditions[rangeKey] = types.Condition{
			AttributeValueList: []types.AttributeValue{
				&types.AttributeValueMemberB{Value: query.RangeValueStart},
			},
			ComparisonOperator: types.ComparisonOperatorGe,
		}
	}

	// Filters
	if query.ValueEqual != nil {
		input.FilterExpression = aws.String(fmt.Sprintf("%s = :v", valueKey))
		input.ExpressionAttributeValues = map[string]types.AttributeValue{
			":v": &types.AttributeValueMemberB{
				Value: query.ValueEqual,
			},
		}
	}

	pageCount := 0
	defer func() {
		a.metrics.dynamoQueryPagesCount.Observe(float64(pageCount))
	}()

	retryer := newRetryer(ctx, a.cfg.BackoffConfig)
	err := instrument.CollectedRequest(ctx, "DynamoDB.QueryPages", a.metrics.dynamoRequestDuration, instrument.ErrorCode, func(innerCtx context.Context) error {
		span := trace.SpanFromContext(innerCtx)
		span.SetAttributes(
			attribute.String("tableName", query.TableName),
			attribute.String("hashValue", query.HashValue),
		)
		output, err := a.DynamoDB.Query(innerCtx, input, func(o *dynamodb.Options) { o.Retryer = retryer })
		pageCount++
		span.SetAttributes(attribute.Int("page", pageCount))
		if cc := output.ConsumedCapacity; cc != nil {
			a.metrics.dynamoConsumedCapacity.WithLabelValues("DynamoDB.QueryPages", *cc.TableName).
				Add(*cc.CapacityUnits)
		}
		callback(query, &dynamoDBReadResponse{items: output.Items})
		return err
	})
	if err != nil {
		return errors.Wrapf(err, "QueryPages error: table=%v", query.TableName)
	}
	return nil
}

type dynamoDBRequest interface {
	Send() error
	Data() interface{}
	Error() error
	Retryable() bool
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
	log, ctx := spanlogger.NewOTel(ctx, log.Logger, tracer, "GetChunks.DynamoDB",
		"numChunks", len(chunks),
	)
	defer log.Finish()
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
	log, ctx := spanlogger.NewOTel(ctx, log.Logger, tracer, "getDynamoDBChunks",
		"numChunks", len(chunks),
	)
	defer log.Finish()
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

		request := dynamodb.BatchGetItemInput{
			RequestItems:           requests,
			ReturnConsumedCapacity: types.ReturnConsumedCapacityTotal,
		}
		var resp *dynamodb.BatchGetItemOutput
		err := instrument.CollectedRequest(ctx, "DynamoDB.BatchGetItemPages", a.metrics.dynamoRequestDuration, instrument.ErrorCode, func(_ context.Context) error {
			var getErr error
			resp, getErr = a.DynamoDB.BatchGetItem(ctx, &request)
			return getErr
		})
		if resp != nil {
			for _, cc := range resp.ConsumedCapacity {
				a.metrics.dynamoConsumedCapacity.WithLabelValues("DynamoDB.BatchGetItemPages", *cc.TableName).
					Add(*cc.CapacityUnits)
			}
		}

		if err != nil {
			for tableName := range requests {
				recordDynamoError(tableName, err, "DynamoDB.BatchGetItemPages", a.metrics)
			}

			// If we get provisionedThroughputExceededException, then no items were processed,
			// so back off and retry all.
			var err2 *types.ProvisionedThroughputExceededException
			var err3 smithy.APIError
			if errors.As(err, &err2) {
				unprocessed.TakeReqs(requests, -1)
				backoff.Wait()
				continue
			} else if errors.As(err, &err3) && err3.ErrorCode() == "ValidationError" {
				// this read will never work, so the only option is to drop the offending request and continue.
				level.Warn(log).Log("msg", "Error while fetching data from Dynamo", "err", err3)
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

		processedChunks, err := processChunkResponse(resp, chunksByKey)
		if err != nil {
			return nil, log.Error(err)
		}
		result = append(result, processedChunks...)

		// If there are unprocessed items, retry those items.
		if unprocessedKeys := resp.UnprocessedKeys; unprocessedKeys != nil && dynamoDBReadRequest(unprocessedKeys).Len() > 0 {
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
			if !ok || key == nil || key.(*types.AttributeValueMemberS).Value == "" {
				return nil, fmt.Errorf("got response from DynamoDB with no hash key: %+v", item)
			}

			chunk, ok := chunksByKey[key.(*types.AttributeValueMemberS).Value]
			if !ok {
				return nil, fmt.Errorf("got response from DynamoDB with chunk I didn't ask for: %s", key.(*types.AttributeValueMemberS).Value)
			}

			buf, ok := item[valueKey]
			if !ok || buf == nil || buf.(*types.AttributeValueMemberB).Value == nil {
				return nil, fmt.Errorf("got response from DynamoDB with no value: %+v", item)
			}

			if err := chunk.Decode(decodeContext, buf.(*types.AttributeValueMemberB).Value); err != nil {
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
	items []map[string]types.AttributeValue
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
	return b.items[b.i][rangeKey].(*types.AttributeValueMemberB).Value
}

func (b *dynamoDBReadResponseIterator) Value() []byte {
	chunkValue, ok := b.items[b.i][valueKey]
	if !ok {
		return nil
	}
	return (chunkValue).(*types.AttributeValueMemberB).Value
}

// map key is table name; value is a slice of things to 'put'
type dynamoDBWriteBatch map[string][]types.WriteRequest

func (b dynamoDBWriteBatch) Len() int {
	result := 0
	for _, reqs := range b {
		result += len(reqs)
	}
	return result
}

// Taken from AWS internal package
func reqToString(req types.WriteRequest) string {
	var buf bytes.Buffer
	reqToBuffer(reflect.ValueOf(req), 0, &buf)
	return buf.String()
}

// Taken from AWS internal package
func reqToBuffer(v reflect.Value, indent int, buf *bytes.Buffer) {
	for v.Kind() == reflect.Ptr {
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Struct:
		buf.WriteString("{\n")

		for i := 0; i < v.Type().NumField(); i++ {
			ft := v.Type().Field(i)
			fv := v.Field(i)

			if ft.Name[0:1] == strings.ToLower(ft.Name[0:1]) {
				continue // ignore unexported fields
			}
			if (fv.Kind() == reflect.Ptr || fv.Kind() == reflect.Slice) && fv.IsNil() {
				continue // ignore unset fields
			}

			buf.WriteString(strings.Repeat(" ", indent+2))
			buf.WriteString(ft.Name + ": ")

			if tag := ft.Tag.Get("sensitive"); tag == "true" {
				buf.WriteString("<sensitive>")
			} else {
				reqToBuffer(fv, indent+2, buf)
			}

			buf.WriteString(",\n")
		}

		buf.WriteString("\n" + strings.Repeat(" ", indent) + "}")
	case reflect.Slice:
		nl, id, id2 := "", "", ""
		if v.Len() > 3 {
			nl, id, id2 = "\n", strings.Repeat(" ", indent), strings.Repeat(" ", indent+2)
		}
		buf.WriteString("[" + nl)
		for i := 0; i < v.Len(); i++ {
			buf.WriteString(id2)
			reqToBuffer(v.Index(i), indent+2, buf)

			if i < v.Len()-1 {
				buf.WriteString("," + nl)
			}
		}

		buf.WriteString(nl + id + "]")
	case reflect.Map:
		buf.WriteString("{\n")

		for i, k := range v.MapKeys() {
			buf.WriteString(strings.Repeat(" ", indent+2))
			buf.WriteString(k.String() + ": ")
			reqToBuffer(v.MapIndex(k), indent+2, buf)

			if i < v.Len()-1 {
				buf.WriteString(",\n")
			}
		}

		buf.WriteString("\n" + strings.Repeat(" ", indent) + "}")
	default:
		format := "%v"
		switch v.Interface().(type) {
		case string:
			format = "%q"
		}
		fmt.Fprintf(buf, format, v.Interface())
	}
}

func (b dynamoDBWriteBatch) String() string {
	var sb strings.Builder
	sb.WriteByte('{')
	for k, reqs := range b {
		sb.WriteString(k)
		sb.WriteString(": [")
		for _, req := range reqs {
			sb.WriteString(reqToString(req))
			sb.WriteByte(',')
		}
		sb.WriteString("], ")
	}
	sb.WriteByte('}')
	return sb.String()
}

func (b dynamoDBWriteBatch) Add(tableName, hashValue string, rangeValue []byte, value []byte) {
	item := map[string]types.AttributeValue{
		hashKey:  &types.AttributeValueMemberS{Value: hashValue},
		rangeKey: &types.AttributeValueMemberB{Value: rangeValue},
	}

	if value != nil {
		item[valueKey] = &types.AttributeValueMemberB{Value: value}
	}

	b[tableName] = append(b[tableName], types.WriteRequest{
		PutRequest: &types.PutRequest{
			Item: item,
		},
	})
}

func (b dynamoDBWriteBatch) Delete(tableName, hashValue string, rangeValue []byte) {
	b[tableName] = append(b[tableName], types.WriteRequest{
		DeleteRequest: &types.DeleteRequest{
			Key: map[string]types.AttributeValue{
				hashKey:  &types.AttributeValueMemberS{Value: hashValue},
				rangeKey: &types.AttributeValueMemberB{Value: rangeValue},
			},
		},
	})
}

// Fill 'b' with WriteRequests from 'from' until 'b' has at most maxVal requests. Remove those requests from 'from'.
func (b dynamoDBWriteBatch) TakeReqs(from dynamoDBWriteBatch, maxVal int) {
	outLen, inLen := b.Len(), from.Len()
	toFill := inLen
	if maxVal > 0 {
		toFill = min(inLen, maxVal-outLen)
	}
	for toFill > 0 {
		for tableName, fromReqs := range from {
			taken := min(len(fromReqs), toFill)
			if taken > 0 {
				b[tableName] = append(b[tableName], fromReqs[:taken]...)
				from[tableName] = fromReqs[taken:]
				toFill -= taken
			}
		}
	}
}

// map key is table name
type dynamoDBReadRequest map[string]types.KeysAndAttributes

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
		requests = types.KeysAndAttributes{
			AttributesToGet: []string{
				hashKey,
				valueKey,
			},
		}
		b[tableName] = requests
	}
	requests.Keys = append(requests.Keys, map[string]types.AttributeValue{
		hashKey:  &types.AttributeValueMemberS{Value: hashValue},
		rangeKey: &types.AttributeValueMemberB{Value: rangeValue},
	})
	b[tableName] = requests
}

// Fill 'b' with ReadRequests from 'from' until 'b' has at most maxVal requests. Remove those requests from 'from'.
func (b dynamoDBReadRequest) TakeReqs(from dynamoDBReadRequest, maxVal int) {
	outLen, inLen := b.Len(), from.Len()
	toFill := inLen
	if maxVal > 0 {
		toFill = min(inLen, maxVal-outLen)
	}
	for toFill > 0 {
		for tableName, fromReqs := range from {
			taken := min(len(fromReqs.Keys), toFill)
			if taken > 0 {
				if _, ok := b[tableName]; !ok {
					b[tableName] = types.KeysAndAttributes{
						AttributesToGet: []string{
							hashKey,
							valueKey,
						},
					}
				}

				tmp := b[tableName]
				tmp.Keys = append(b[tableName].Keys, fromReqs.Keys[:taken]...)
				b[tableName] = tmp
				tmp = from[tableName]
				tmp.Keys = fromReqs.Keys[taken:]
				from[tableName] = tmp
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
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		metrics.dynamoFailures.WithLabelValues(tableName, apiErr.ErrorCode(), operation).Add(float64(1))
	} else {
		metrics.dynamoFailures.WithLabelValues(tableName, otherError, operation).Add(float64(1))
	}
}

// dynamoClientFromURL creates a new DynamoDB client from a URL.
func dynamoClientFromURL(awsURL *url.URL) (dynamodb.Client, error) {
	dynamoDBSession, err := dynamoOptionsFromURL(awsURL)
	if err != nil {
		return dynamodb.Client{}, err
	}
	return *dynamodb.New(dynamoDBSession), nil
}

// dynamoOptionsFromURL creates a new dynamodb.Options object from a URL.
func dynamoOptionsFromURL(awsURL *url.URL) (dynamodb.Options, error) {
	if awsURL == nil {
		return dynamodb.Options{}, fmt.Errorf("no URL specified for DynamoDB")
	}
	path := strings.TrimPrefix(awsURL.Path, "/")
	if len(path) > 0 {
		level.Warn(log.Logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
	}
	config, err := dynamodbOptionsFromURL(awsURL)
	if err != nil {
		return dynamodb.Options{}, err
	}
	config.RetryMaxAttempts = 0 // We do our own retries, so we can monitor them
	return *config, nil
}

// Deprecated: Has been copied from ConfigFromURL which was used for generating the configuration for both S3 and DynamoDB
// when the Amazon AWS SKD v1 was still used.
func dynamodbOptionsFromURL(awsURL *url.URL) (*dynamodb.Options, error) {
	httpClient := &http.Client{
		Transport: &http.Transport{
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
		},
	}
	config := dynamodb.Options{HTTPClient: httpClient}

	// Use a custom http.Client with the golang defaults but also specifying
	// MaxIdleConnsPerHost because of a bug in golang https://github.com/golang/go/issues/13801
	// where MaxIdleConnsPerHost does not work as expected.

	if awsURL.User != nil {
		key, secret := credentialsFromURL(awsURL)

		// We request at least the username or password being set to enable the static credentials.
		if key != "" || secret != "" {
			config.Credentials = credentials.NewStaticCredentialsProvider(key, secret, "")
		}
	}

	if strings.Contains(awsURL.Host, ".") {
		region := os.Getenv("AWS_REGION")
		if region == "" {
			region = InvalidAWSRegion
		}
		config.Region = region
		if awsURL.Scheme == "https" {
			config.BaseEndpoint = aws.String(fmt.Sprintf("%s://%s", awsURL.Scheme, awsURL.Host))
		} else {
			config.BaseEndpoint = aws.String(fmt.Sprintf("http://%s", awsURL.Host))
		}
	} else {
		config.Region = awsURL.Host
	}
	// Let AWS generate default endpoint based on region passed as a host in URL.
	return &config, nil
}

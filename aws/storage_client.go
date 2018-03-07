package aws

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io/ioutil"
	"net/url"
	"strings"
	"time"

	"github.com/go-kit/kit/log/level"
	ot "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/prometheus/client_golang/prometheus"

	awscommon "github.com/weaveworks/common/aws"
	"github.com/weaveworks/common/instrument"
	"github.com/weaveworks/cortex/pkg/chunk"
	"github.com/weaveworks/cortex/pkg/util"
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
)

var backoffConfig = util.BackoffConfig{
	// Backoff for dynamoDB requests, to match AWS lib - see:
	// https://github.com/aws/aws-sdk-go/blob/master/service/dynamodb/customizations.go
	MinBackoff: 100 * time.Millisecond,
	MaxBackoff: 50 * time.Second,
	MaxRetries: 20,
}

var (
	dynamoRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "dynamo_request_duration_seconds",
		Help:      "Time spent doing DynamoDB requests.",

		// DynamoDB latency seems to range from a few ms to a few sec and is
		// important.  So use 8 buckets from 128us to 2s.
		Buckets: prometheus.ExponentialBuckets(0.000128, 4, 8),
	}, []string{"operation", "status_code"})
	dynamoConsumedCapacity = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "dynamo_consumed_capacity_total",
		Help:      "The capacity units consumed by operation.",
	}, []string{"operation", tableNameLabel})
	dynamoFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "dynamo_failures_total",
		Help:      "The total number of errors while storing chunks to the chunk store.",
	}, []string{tableNameLabel, errorReasonLabel, "operation"})
	dynamoQueryPagesCount = prometheus.NewHistogram(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "dynamo_query_pages_count",
		Help:      "Number of pages per query.",
		// Most queries will have one page, however this may increase with fuzzy
		// metric names.
		Buckets: prometheus.ExponentialBuckets(1, 4, 6),
	})
	dynamoQueryRetryCount = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "dynamo_query_retry_count",
		Help:      "Number of retries per DynamoDB operation.",
		Buckets:   prometheus.LinearBuckets(0, 1, 21),
	}, []string{"operation"})
	s3RequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "s3_request_duration_seconds",
		Help:      "Time spent doing S3 requests.",
		Buckets:   []float64{.025, .05, .1, .25, .5, 1, 2},
	}, []string{"operation", "status_code"})
)

func init() {
	prometheus.MustRegister(dynamoRequestDuration)
	prometheus.MustRegister(dynamoConsumedCapacity)
	prometheus.MustRegister(dynamoFailures)
	prometheus.MustRegister(dynamoQueryPagesCount)
	prometheus.MustRegister(dynamoQueryRetryCount)
	prometheus.MustRegister(s3RequestDuration)
}

// DynamoDBConfig specifies config for a DynamoDB database.
type DynamoDBConfig struct {
	DynamoDB               util.URLValue
	APILimit               float64
	ApplicationAutoScaling util.URLValue
	ChunkGangSize          int
	ChunkGetMaxParallelism int
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *DynamoDBConfig) RegisterFlags(f *flag.FlagSet) {
	f.Var(&cfg.DynamoDB, "dynamodb.url", "DynamoDB endpoint URL with escaped Key and Secret encoded. "+
		"If only region is specified as a host, proper endpoint will be deduced. Use inmemory:///<table-name> to use a mock in-memory implementation.")
	f.Float64Var(&cfg.APILimit, "dynamodb.api-limit", 2.0, "DynamoDB table management requests per second limit.")
	f.Var(&cfg.ApplicationAutoScaling, "applicationautoscaling.url", "ApplicationAutoscaling endpoint URL with escaped Key and Secret encoded.")
	f.IntVar(&cfg.ChunkGangSize, "dynamodb.chunk.gang.size", 10, "Number of chunks to group together to parallelise fetches (zero to disable)")
	f.IntVar(&cfg.ChunkGetMaxParallelism, "dynamodb.chunk.get.max.parallelism", 32, "Max number of chunk-get operations to start in parallel")
}

// StorageConfig specifies config for storing data on AWS.
type StorageConfig struct {
	DynamoDBConfig
	S3 util.URLValue
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *StorageConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.DynamoDBConfig.RegisterFlags(f)

	f.Var(&cfg.S3, "s3.url", "S3 endpoint URL with escaped Key and Secret encoded. "+
		"If only region is specified as a host, proper endpoint will be deduced. Use inmemory:///<bucket-name> to use a mock in-memory implementation.")
}

type storageClient struct {
	cfg       StorageConfig
	schemaCfg chunk.SchemaConfig

	DynamoDB   dynamodbiface.DynamoDBAPI
	S3         s3iface.S3API
	bucketName string

	// These functions exists for mocking, so we don't have to write a whole load
	// of boilerplate.
	queryRequestFn          func(ctx context.Context, input *dynamodb.QueryInput) dynamoDBRequest
	batchGetItemRequestFn   func(ctx context.Context, input *dynamodb.BatchGetItemInput) dynamoDBRequest
	batchWriteItemRequestFn func(ctx context.Context, input *dynamodb.BatchWriteItemInput) dynamoDBRequest
}

// NewStorageClient makes a new AWS-backed StorageClient.
func NewStorageClient(cfg StorageConfig, schemaCfg chunk.SchemaConfig) (chunk.StorageClient, error) {
	dynamoDB, err := dynamoClientFromURL(cfg.DynamoDB.URL)
	if err != nil {
		return nil, err
	}

	if cfg.S3.URL == nil {
		return nil, fmt.Errorf("no URL specified for S3")
	}
	s3Config, err := awscommon.ConfigFromURL(cfg.S3.URL)
	if err != nil {
		return nil, err
	}
	s3Config = s3Config.WithMaxRetries(0) // We do our own retries, so we can monitor them
	s3Client := s3.New(session.New(s3Config))
	bucketName := strings.TrimPrefix(cfg.S3.URL.Path, "/")

	client := storageClient{
		cfg:        cfg,
		schemaCfg:  schemaCfg,
		DynamoDB:   dynamoDB,
		S3:         s3Client,
		bucketName: bucketName,
	}
	client.queryRequestFn = client.queryRequest
	client.batchGetItemRequestFn = client.batchGetItemRequest
	client.batchWriteItemRequestFn = client.batchWriteItemRequest
	return client, nil
}

func (a storageClient) NewWriteBatch() chunk.WriteBatch {
	return dynamoDBWriteBatch(map[string][]*dynamodb.WriteRequest{})
}

// BatchWrite writes requests to the underlying storage, handling retries and backoff.
// Structure is identical to getDynamoDBChunks(), but operating on different datatypes
// so cannot share implementation.  If you fix a bug here fix it there too.
func (a storageClient) BatchWrite(ctx context.Context, input chunk.WriteBatch) error {
	outstanding := input.(dynamoDBWriteBatch)
	unprocessed := dynamoDBWriteBatch{}

	backoff := util.NewBackoff(ctx, backoffConfig)
	defer func() {
		dynamoQueryRetryCount.WithLabelValues("BatchWrite").Observe(float64(backoff.NumRetries()))
	}()

	for outstanding.Len()+unprocessed.Len() > 0 && backoff.Ongoing() {
		requests := dynamoDBWriteBatch{}
		requests.TakeReqs(outstanding, dynamoDBMaxWriteBatchSize)
		requests.TakeReqs(unprocessed, dynamoDBMaxWriteBatchSize)

		request := a.batchWriteItemRequestFn(ctx, &dynamodb.BatchWriteItemInput{
			RequestItems:           requests,
			ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
		})

		err := instrument.TimeRequestHistogram(ctx, "DynamoDB.BatchWriteItem", dynamoRequestDuration, func(ctx context.Context) error {
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
				unprocessed.TakeReqs(requests, -1)
				backoff.Wait()
				continue
			}

			// All other errors are critical.
			return err
		}

		// If there are unprocessed items, backoff and retry those items.
		if unprocessedItems := resp.UnprocessedItems; unprocessedItems != nil && dynamoDBWriteBatch(unprocessedItems).Len() > 0 {
			unprocessed.TakeReqs(unprocessedItems, -1)
			// I am unclear why we don't count here; perhaps the idea is
			// that while we are making _some_ progress we should carry on.
			backoff.WaitWithoutCounting()
			continue
		}

		backoff.Reset()
	}

	if valuesLeft := outstanding.Len() + unprocessed.Len(); valuesLeft > 0 {
		if valuesLeft < 4 { // protect against logging lots of data
			level.Info(util.Logger).Log("msg", "DynamoDB BatchWrite values left", "count", valuesLeft, "outstanding", outstanding, "unprocessed", unprocessed)
		}
		return fmt.Errorf("failed to write chunk, %d values remaining: %s", valuesLeft, backoff.Err())
	}
	return backoff.Err()
}

func (a storageClient) QueryPages(ctx context.Context, query chunk.IndexQuery, callback func(result chunk.ReadBatch, lastPage bool) (shouldContinue bool)) error {
	sp, ctx := ot.StartSpanFromContext(ctx, "QueryPages", ot.Tag{Key: "tableName", Value: query.TableName}, ot.Tag{Key: "hashValue", Value: query.HashValue})
	defer sp.Finish()

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

		response, err := a.queryPage(ctx, input, page)
		if err != nil {
			return err
		}

		if getNextPage := callback(response, !page.HasNextPage()); !getNextPage {
			if err != nil {
				return fmt.Errorf("QueryPages error: table=%v, err=%v", *input.TableName, page.Error())
			}
			return nil
		}
	}
	return nil
}

func (a storageClient) queryPage(ctx context.Context, input *dynamodb.QueryInput, page dynamoDBRequest) (dynamoDBReadResponse, error) {
	backoff := util.NewBackoff(ctx, backoffConfig)
	defer func() {
		dynamoQueryRetryCount.WithLabelValues("queryPage").Observe(float64(backoff.NumRetries()))
	}()

	var err error
	for backoff.Ongoing() {
		err = instrument.TimeRequestHistogram(ctx, "DynamoDB.QueryPages", dynamoRequestDuration, func(_ context.Context) error {
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
		return dynamoDBReadResponse(queryOutput.Items), nil
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

func (a storageClient) queryRequest(ctx context.Context, input *dynamodb.QueryInput) dynamoDBRequest {
	req, _ := a.DynamoDB.QueryRequest(input)
	req.SetContext(ctx)
	return dynamoDBRequestAdapter{req}
}

func (a storageClient) batchGetItemRequest(ctx context.Context, input *dynamodb.BatchGetItemInput) dynamoDBRequest {
	req, _ := a.DynamoDB.BatchGetItemRequest(input)
	req.SetContext(ctx)
	return dynamoDBRequestAdapter{req}
}

func (a storageClient) batchWriteItemRequest(ctx context.Context, input *dynamodb.BatchWriteItemInput) dynamoDBRequest {
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
	return *a.request.Retryable
}

type chunksPlusError struct {
	chunks []chunk.Chunk
	err    error
}

func (a storageClient) GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	sp, ctx := ot.StartSpanFromContext(ctx, "GetChunks")
	defer sp.Finish()
	sp.LogFields(otlog.Int("chunks requested", len(chunks)))

	var (
		s3Chunks       []chunk.Chunk
		dynamoDBChunks []chunk.Chunk
	)

	for _, chunk := range chunks {
		if !a.schemaCfg.ChunkTables.From.IsSet() || chunk.From.Before(a.schemaCfg.ChunkTables.From.Time) {
			s3Chunks = append(s3Chunks, chunk)
		} else {
			dynamoDBChunks = append(dynamoDBChunks, chunk)
		}
	}

	// Get chunks from S3, then get chunks from DynamoDB.  I don't expect us to be
	// doing both simultaneously except for when we migrate, when it will only
	// occur for a couple or hours. So I didn't think it is worth the extra code
	// to parallelise.

	var err error
	s3Chunks, err = a.getS3Chunks(ctx, s3Chunks)
	if err != nil {
		return s3Chunks, err
	}

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
	finalChunks := s3Chunks
	for i := 0; i < len(dynamoDBChunks); i += gangSize {
		in := <-results
		if in.err != nil {
			err = in.err // TODO: cancel other sub-queries at this point
		} else {
			finalChunks = append(finalChunks, in.chunks...)
		}
	}
	sp.LogFields(otlog.Int("chunks fetched", len(finalChunks)))
	if err != nil {
		sp.LogFields(otlog.String("error", err.Error()))
		return nil, err
	}

	// Return any chunks we did receive: a partial result may be useful
	return finalChunks, err
}

func (a storageClient) getS3Chunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	incomingChunks := make(chan chunk.Chunk)
	incomingErrors := make(chan error)
	for _, c := range chunks {
		go func(c chunk.Chunk) {
			c, err := a.getS3Chunk(ctx, c)
			if err != nil {
				incomingErrors <- err
				return
			}
			incomingChunks <- c
		}(c)
	}

	result := []chunk.Chunk{}
	errors := []error{}
	for i := 0; i < len(chunks); i++ {
		select {
		case chunk := <-incomingChunks:
			result = append(result, chunk)
		case err := <-incomingErrors:
			errors = append(errors, err)
		}
	}
	if len(errors) > 0 {
		// Return any chunks we did receive: a partial result may be useful
		return result, errors[0]
	}
	return result, nil
}

func (a storageClient) getS3Chunk(ctx context.Context, c chunk.Chunk) (chunk.Chunk, error) {
	var resp *s3.GetObjectOutput
	err := instrument.TimeRequestHistogram(ctx, "S3.GetObject", s3RequestDuration, func(ctx context.Context) error {
		var err error
		resp, err = a.S3.GetObjectWithContext(ctx, &s3.GetObjectInput{
			Bucket: aws.String(a.bucketName),
			Key:    aws.String(c.ExternalKey()),
		})
		return err
	})
	if err != nil {
		return chunk.Chunk{}, err
	}
	defer resp.Body.Close()
	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return chunk.Chunk{}, err
	}
	decodeContext := chunk.NewDecodeContext()
	if err := c.Decode(decodeContext, buf); err != nil {
		return chunk.Chunk{}, err
	}
	return c, nil
}

// As we're re-using the DynamoDB schema from the index for the chunk tables,
// we need to provide a non-null, non-empty value for the range value.
var placeholder = []byte{'c'}

// Fetch a set of chunks from DynamoDB, handling retries and backoff.
// Structure is identical to BatchWrite(), but operating on different datatypes
// so cannot share implementation.  If you fix a bug here fix it there too.
func (a storageClient) getDynamoDBChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error) {
	sp, ctx := ot.StartSpanFromContext(ctx, "getDynamoDBChunks", ot.Tag{Key: "numChunks", Value: len(chunks)})
	defer sp.Finish()
	outstanding := dynamoDBReadRequest{}
	chunksByKey := map[string]chunk.Chunk{}
	for _, chunk := range chunks {
		key := chunk.ExternalKey()
		chunksByKey[key] = chunk
		tableName := a.schemaCfg.ChunkTables.TableFor(chunk.From)
		outstanding.Add(tableName, key, placeholder)
	}

	result := []chunk.Chunk{}
	unprocessed := dynamoDBReadRequest{}
	backoff := util.NewBackoff(ctx, backoffConfig)
	defer func() {
		dynamoQueryRetryCount.WithLabelValues("getDynamoDBChunks").Observe(float64(backoff.NumRetries()))
	}()

	for outstanding.Len()+unprocessed.Len() > 0 && backoff.Ongoing() {
		requests := dynamoDBReadRequest{}
		requests.TakeReqs(outstanding, dynamoDBMaxReadBatchSize)
		requests.TakeReqs(unprocessed, dynamoDBMaxReadBatchSize)

		request := a.batchGetItemRequestFn(ctx, &dynamodb.BatchGetItemInput{
			RequestItems:           requests,
			ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
		})

		err := instrument.TimeRequestHistogram(ctx, "DynamoDB.BatchGetItemPages", dynamoRequestDuration, func(ctx context.Context) error {
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
			}

			// All other errors are critical.
			return nil, err
		}

		processedChunks, err := processChunkResponse(response, chunksByKey)
		if err != nil {
			return nil, err
		}
		result = append(result, processedChunks...)

		// If there are unprocessed items, backoff and retry those items.
		if unprocessedKeys := response.UnprocessedKeys; unprocessedKeys != nil && dynamoDBReadRequest(unprocessedKeys).Len() > 0 {
			unprocessed.TakeReqs(unprocessedKeys, -1)
			// I am unclear why we don't count here; perhaps the idea is
			// that while we are making _some_ progress we should carry on.
			backoff.WaitWithoutCounting()
			continue
		}

		backoff.Reset()
	}

	if valuesLeft := outstanding.Len() + unprocessed.Len(); valuesLeft > 0 {
		// Return the chunks we did fetch, because partial results may be useful
		return result, fmt.Errorf("failed to query chunks, %d values remaining: %s", valuesLeft, backoff.Err())
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

func (a storageClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	var (
		s3ChunkKeys    []string
		s3ChunkBufs    [][]byte
		dynamoDBWrites = dynamoDBWriteBatch{}
	)

	for i := range chunks {
		// Encode the chunk first - checksum is calculated as a side effect.
		buf, err := chunks[i].Encode()
		if err != nil {
			return err
		}
		key := chunks[i].ExternalKey()

		if !a.schemaCfg.ChunkTables.From.IsSet() || chunks[i].From.Before(a.schemaCfg.ChunkTables.From.Time) {
			s3ChunkKeys = append(s3ChunkKeys, key)
			s3ChunkBufs = append(s3ChunkBufs, buf)
		} else {
			table := a.schemaCfg.ChunkTables.TableFor(chunks[i].From)
			dynamoDBWrites.Add(table, key, placeholder, buf)
		}
	}

	// Put chunks to S3, then put chunks to DynamoDB.  I don't expect us to be
	// doing both simultaneously except for when we migrate, when it will only
	// occur for a couple or hours. So I didn't think it is worth the extra code
	// to parallelise.

	if err := a.putS3Chunks(ctx, s3ChunkKeys, s3ChunkBufs); err != nil {
		return err
	}

	return a.BatchWrite(ctx, dynamoDBWrites)
}

func (a storageClient) putS3Chunks(ctx context.Context, keys []string, bufs [][]byte) error {
	incomingErrors := make(chan error)
	for i := range bufs {
		go func(i int) {
			incomingErrors <- a.putS3Chunk(ctx, keys[i], bufs[i])
		}(i)
	}

	var lastErr error
	for range keys {
		err := <-incomingErrors
		if err != nil {
			lastErr = err
		}
	}
	return lastErr
}

func (a storageClient) putS3Chunk(ctx context.Context, key string, buf []byte) error {
	return instrument.TimeRequestHistogram(ctx, "S3.PutObject", s3RequestDuration, func(ctx context.Context) error {
		_, err := a.S3.PutObjectWithContext(ctx, &s3.PutObjectInput{
			Body:   bytes.NewReader(buf),
			Bucket: aws.String(a.bucketName),
			Key:    aws.String(key),
		})
		return err
	})
}

// Slice of values returned; map key is attribute name
type dynamoDBReadResponse []map[string]*dynamodb.AttributeValue

func (b dynamoDBReadResponse) Len() int {
	return len(b)
}

func (b dynamoDBReadResponse) RangeValue(i int) []byte {
	return b[i][rangeKey].B
}

func (b dynamoDBReadResponse) Value(i int) []byte {
	chunkValue, ok := b[i][valueKey]
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

func (b dynamoDBWriteBatch) String() string {
	buf := &bytes.Buffer{}
	for table, reqs := range b {
		for _, req := range reqs {
			item := req.PutRequest.Item
			hash := ""
			if hashAttr, ok := item[hashKey]; ok {
				if hashAttr.S != nil {
					hash = *hashAttr.S
				}
			}
			var rnge, value []byte
			if rangeAttr, ok := item[rangeKey]; ok {
				rnge = rangeAttr.B
			}
			if valueAttr, ok := item[valueKey]; ok {
				value = valueAttr.B
			}
			fmt.Fprintf(buf, "%s: %s,%.32s,%.32s; ", table, hash, rnge, value)
		}
	}
	return buf.String()
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
			ConsistentRead: aws.Bool(true),
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
						ConsistentRead: aws.Bool(true),
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

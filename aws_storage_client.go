package chunk

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/url"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3iface"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/log"
	"golang.org/x/net/context"

	"github.com/weaveworks/common/instrument"
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

	provisionedThroughputExceededException = "ProvisionedThroughputExceededException"

	// Backoff for dynamoDB requests, to match AWS lib - see:
	// https://github.com/aws/aws-sdk-go/blob/master/service/dynamodb/customizations.go
	minBackoff = 50 * time.Millisecond
	maxBackoff = 50 * time.Second
	maxRetries = 20

	// See http://docs.aws.amazon.com/amazondynamodb/latest/developerguide/Limits.html.
	dynamoMaxBatchSize = 25
)

var (
	dynamoRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "cortex",
		Name:      "dynamo_request_duration_seconds",
		Help:      "Time spent doing DynamoDB requests.",

		// DynamoDB latency seems to range from a few ms to a few sec and is
		// important.  So use 8 buckets from 64us to 8s.
		Buckets: prometheus.ExponentialBuckets(0.000128, 4, 8),
	}, []string{"operation", "status_code"})
	dynamoConsumedCapacity = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "dynamo_consumed_capacity_total",
		Help:      "The capacity units consumed by operation.",
	}, []string{"operation"})
	dynamoFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "dynamo_failures_total",
		Help:      "The total number of errors while storing chunks to the chunk store.",
	}, []string{tableNameLabel, errorReasonLabel})
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
	prometheus.MustRegister(s3RequestDuration)
}

// DynamoDBConfig specifies config for a DynamoDB database.
type DynamoDBConfig struct {
	DynamoDB util.URLValue
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *DynamoDBConfig) RegisterFlags(f *flag.FlagSet) {
	f.Var(&cfg.DynamoDB, "dynamodb.url", "DynamoDB endpoint URL with escaped Key and Secret encoded. "+
		"If only region is specified as a host, proper endpoint will be deduced. Use inmemory:///<table-name> to use a mock in-memory implementation.")
}

// AWSStorageConfig specifies config for storing data on AWS.
type AWSStorageConfig struct {
	DynamoDBConfig
	S3 util.URLValue
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *AWSStorageConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.DynamoDBConfig.RegisterFlags(f)
	f.Var(&cfg.S3, "s3.url", "S3 endpoint URL with escaped Key and Secret encoded. "+
		"If only region is specified as a host, proper endpoint will be deduced. Use inmemory:///<bucket-name> to use a mock in-memory implementation.")
}

type awsStorageClient struct {
	DynamoDB   dynamodbiface.DynamoDBAPI
	S3         s3iface.S3API
	bucketName string

	// queryRequestFn exists for mocking, so we don't have to write a whole load
	// of boilerplate.
	queryRequestFn func(ctx context.Context, input *dynamodb.QueryInput) dynamoDBRequest
}

// NewAWSStorageClient makes a new AWS-backed StorageClient.
func NewAWSStorageClient(cfg AWSStorageConfig) (StorageClient, error) {
	dynamoDB, err := dynamoClientFromURL(cfg.DynamoDB.URL)
	if err != nil {
		return nil, err
	}

	if cfg.S3.URL == nil {
		return nil, fmt.Errorf("no URL specified for S3")
	}
	s3Config, err := awsConfigFromURL(cfg.S3.URL)
	if err != nil {
		return nil, err
	}
	s3Client := s3.New(session.New(s3Config))
	bucketName := strings.TrimPrefix(cfg.S3.URL.Path, "/")

	storageClient := awsStorageClient{
		DynamoDB:   dynamoDB,
		S3:         s3Client,
		bucketName: bucketName,
	}
	storageClient.queryRequestFn = storageClient.queryRequest
	return storageClient, nil
}

func (a awsStorageClient) NewWriteBatch() WriteBatch {
	return dynamoDBWriteBatch(map[string][]*dynamodb.WriteRequest{})
}

// batchWrite writes requests to the underlying storage, handling retires and backoff.
func (a awsStorageClient) BatchWrite(ctx context.Context, input WriteBatch) error {
	outstanding := input.(dynamoDBWriteBatch)
	unprocessed := map[string][]*dynamodb.WriteRequest{}
	backoff, numRetries := minBackoff, 0
	for dictLen(outstanding)+dictLen(unprocessed) > 0 && numRetries < maxRetries {
		reqs := map[string][]*dynamodb.WriteRequest{}
		takeReqs(unprocessed, reqs, dynamoMaxBatchSize)
		takeReqs(outstanding, reqs, dynamoMaxBatchSize)
		var resp *dynamodb.BatchWriteItemOutput

		err := instrument.TimeRequestHistogram(ctx, "DynamoDB.BatchWriteItem", dynamoRequestDuration, func(ctx context.Context) error {
			var err error
			resp, err = a.DynamoDB.BatchWriteItemWithContext(ctx, &dynamodb.BatchWriteItemInput{
				RequestItems:           reqs,
				ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
			})
			return err
		})
		for _, cc := range resp.ConsumedCapacity {
			dynamoConsumedCapacity.WithLabelValues("DynamoDB.BatchWriteItem").
				Add(float64(*cc.CapacityUnits))
		}

		if err != nil {
			for tableName := range reqs {
				recordDynamoError(tableName, err)
			}
		}

		// If there are unprocessed items, backoff and retry those items.
		if unprocessedItems := resp.UnprocessedItems; unprocessedItems != nil && dictLen(unprocessedItems) > 0 {
			takeReqs(unprocessedItems, unprocessed, -1)
			time.Sleep(backoff)
			backoff = nextBackoff(backoff)
			continue
		}

		// If we get provisionedThroughputExceededException, then no items were processed,
		// so back off and retry all.
		if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == provisionedThroughputExceededException {
			takeReqs(reqs, unprocessed, -1)
			time.Sleep(backoff)
			backoff = nextBackoff(backoff)
			numRetries++
			continue
		}

		// All other errors are fatal.
		if err != nil {
			return err
		}

		backoff = minBackoff
		numRetries = 0
	}

	if valuesLeft := dictLen(outstanding) + dictLen(unprocessed); valuesLeft > 0 {
		return fmt.Errorf("failed to write chunk after %d retries, %d values remaining", numRetries, valuesLeft)
	}
	return nil
}

func (a awsStorageClient) QueryPages(ctx context.Context, query IndexQuery, callback func(result ReadBatch, lastPage bool) (shouldContinue bool)) error {
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
	backoff := minBackoff
	for page := request; page != nil; page = page.NextPage() {
		err := instrument.TimeRequestHistogram(ctx, "DynamoDB.QueryPages", dynamoRequestDuration, func(_ context.Context) error {
			return page.Send()
		})

		if cc := page.Data().(*dynamodb.QueryOutput).ConsumedCapacity; cc != nil {
			dynamoConsumedCapacity.WithLabelValues("DynamoDB.QueryPages").
				Add(float64(*cc.CapacityUnits))
		}

		if err != nil {
			recordDynamoError(*input.TableName, err)

			if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == provisionedThroughputExceededException {
				time.Sleep(backoff)
				backoff = nextBackoff(backoff)
				continue
			}

			return fmt.Errorf("QueryPages error: table=%v, err=%v", *input.TableName, err)
		}

		queryOutput := page.Data().(*dynamodb.QueryOutput)
		if getNextPage := callback(dynamoDBReadBatch(queryOutput.Items), !page.HasNextPage()); !getNextPage {
			if err != nil {
				return fmt.Errorf("QueryPages error: table=%v, err=%v", *input.TableName, page.Error())
			}
			return nil
		}

		backoff = minBackoff
	}

	return nil
}

type dynamoDBRequest interface {
	NextPage() dynamoDBRequest
	Send() error
	Data() interface{}
	Error() error
	HasNextPage() bool
}

func (a awsStorageClient) queryRequest(ctx context.Context, input *dynamodb.QueryInput) dynamoDBRequest {
	req, _ := a.DynamoDB.QueryRequest(input)
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
	return a.request.Send()
}

func (a dynamoDBRequestAdapter) Error() error {
	return a.request.Error
}

func (a dynamoDBRequestAdapter) HasNextPage() bool {
	return a.request.HasNextPage()
}

func (a awsStorageClient) GetChunk(ctx context.Context, key string) ([]byte, error) {
	var resp *s3.GetObjectOutput
	err := instrument.TimeRequestHistogram(ctx, "S3.GetObject", s3RequestDuration, func(ctx context.Context) error {
		var err error
		resp, err = a.S3.GetObjectWithContext(ctx, &s3.GetObjectInput{
			Bucket: aws.String(a.bucketName),
			Key:    aws.String(key),
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return buf, nil
}

func (a awsStorageClient) PutChunk(ctx context.Context, key string, buf []byte) error {
	return instrument.TimeRequestHistogram(ctx, "S3.PutObject", s3RequestDuration, func(ctx context.Context) error {
		_, err := a.S3.PutObjectWithContext(ctx, &s3.PutObjectInput{
			Body:   bytes.NewReader(buf),
			Bucket: aws.String(a.bucketName),
			Key:    aws.String(key),
		})
		return err
	})
}

type dynamoDBWriteBatch map[string][]*dynamodb.WriteRequest

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

type dynamoDBReadBatch []map[string]*dynamodb.AttributeValue

func (b dynamoDBReadBatch) Len() int {
	return len(b)
}

func (b dynamoDBReadBatch) RangeValue(i int) []byte {
	return b[i][rangeKey].B
}

func (b dynamoDBReadBatch) Value(i int) []byte {
	chunkValue, ok := b[i][valueKey]
	if !ok {
		return nil
	}
	return chunkValue.B
}

func nextBackoff(lastBackoff time.Duration) time.Duration {
	// Based on the "Decorrelated Jitter" approach from https://www.awsarchitectureblog.com/2015/03/backoff.html
	// sleep = min(cap, random_between(base, sleep * 3))
	backoff := minBackoff + time.Duration(rand.Int63n(int64((lastBackoff*3)-minBackoff)))
	if backoff > maxBackoff {
		backoff = maxBackoff
	}
	return backoff
}

func recordDynamoError(tableName string, err error) {
	if awsErr, ok := err.(awserr.Error); ok {
		dynamoFailures.WithLabelValues(tableName, awsErr.Code()).Add(float64(1))
	} else {
		dynamoFailures.WithLabelValues(tableName, otherError).Add(float64(1))
	}
}

func dictLen(b map[string][]*dynamodb.WriteRequest) int {
	result := 0
	for _, reqs := range b {
		result += len(reqs)
	}
	return result
}

// Fill 'to' with WriteRequests from 'from' until 'to' has at most max requests. Remove those requests from 'from'.
func takeReqs(from, to map[string][]*dynamodb.WriteRequest, max int) {
	outLen, inLen := dictLen(to), dictLen(from)
	toFill := inLen
	if max > 0 {
		toFill = util.Min(inLen, max-outLen)
	}
	for toFill > 0 {
		for tableName, fromReqs := range from {
			taken := util.Min(len(fromReqs), toFill)
			if taken > 0 {
				to[tableName] = append(to[tableName], fromReqs[:taken]...)
				from[tableName] = fromReqs[taken:]
				toFill -= taken
			}
		}
	}
}

// dynamoClientFromURL creates a new DynamoDB client from a URL.
func dynamoClientFromURL(awsURL *url.URL) (dynamodbiface.DynamoDBAPI, error) {
	if awsURL == nil {
		return nil, fmt.Errorf("no URL specified for DynamoDB")
	}
	path := strings.TrimPrefix(awsURL.Path, "/")
	if len(path) > 0 {
		log.Warnf("Ignoring DynamoDB URL path: %v.", path)
	}
	config, err := awsConfigFromURL(awsURL)
	if err != nil {
		return nil, err
	}
	return dynamodb.New(session.New(config)), nil
}

// awsConfigFromURL returns AWS config from given URL. It expects escaped AWS Access key ID & Secret Access Key to be
// encoded in the URL. It also expects region specified as a host (letting AWS generate full endpoint) or fully valid
// endpoint with dummy region assumed (e.g for URLs to emulated services).
func awsConfigFromURL(awsURL *url.URL) (*aws.Config, error) {
	if awsURL.User == nil {
		return nil, fmt.Errorf("must specify escaped Access Key & Secret Access in URL")
	}

	password, _ := awsURL.User.Password()
	creds := credentials.NewStaticCredentials(awsURL.User.Username(), password, "")
	config := aws.NewConfig().
		WithCredentials(creds).
		WithMaxRetries(0) // We do our own retries, so we can monitor them
	if strings.Contains(awsURL.Host, ".") {
		return config.WithEndpoint(fmt.Sprintf("http://%s", awsURL.Host)).WithRegion("dummy"), nil
	}

	// Let AWS generate default endpoint based on region passed as a host in URL.
	return config.WithRegion(awsURL.Host), nil
}

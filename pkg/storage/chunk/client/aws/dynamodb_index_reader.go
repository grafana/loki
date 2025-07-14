package aws

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/client"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	gklog "github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
)

type dynamodbIndexReader struct {
	dynamoDBStorageClient

	log        gklog.Logger
	maxRetries int

	rowsRead prometheus.Counter
}

// NewDynamoDBIndexReader returns an object that can scan an entire index table
func NewDynamoDBIndexReader(cfg DynamoDBConfig, schemaCfg config.SchemaConfig, reg prometheus.Registerer, l gklog.Logger, rowsRead prometheus.Counter) (index.Reader, error) {
	client, err := newDynamoDBStorageClient(cfg, schemaCfg, reg)
	if err != nil {
		return nil, err
	}

	return &dynamodbIndexReader{
		dynamoDBStorageClient: *client,
		maxRetries:            cfg.BackoffConfig.MaxRetries,
		log:                   l,

		rowsRead: rowsRead,
	}, nil
}

func (r *dynamodbIndexReader) IndexTableNames(ctx context.Context) ([]string, error) {
	// fake up a table client - if we call NewDynamoDBTableClient() it will double-register metrics
	tableClient := dynamoTableClient{
		DynamoDB: r.DynamoDB,
		metrics:  r.metrics,
	}
	return tableClient.ListTables(ctx)
}

type seriesMap struct {
	mutex           sync.Mutex           // protect concurrent access to maps
	seriesProcessed map[string]sha256Set // map of userID/bucket to set showing which series have been processed
}

// Since all sha256 values are the same size, a fixed-size array
// is more space-efficient than string or byte slice
type sha256 [32]byte

// an entry in this set indicates we have processed a series with that sha already
type sha256Set struct {
	series map[sha256]struct{}
}

// ReadIndexEntries reads the whole of a table on multiple goroutines in parallel.
// Entries for the same HashValue and RangeValue should be passed to the same processor.
func (r *dynamodbIndexReader) ReadIndexEntries(ctx context.Context, tableName string, processors []index.EntryProcessor) error {
	projection := hashKey + "," + rangeKey

	sm := &seriesMap{ // new map per table
		seriesProcessed: make(map[string]sha256Set),
	}

	var readerGroup errgroup.Group
	// Start a goroutine for each processor
	for segment, processor := range processors {
		readerGroup.Go(func() error {
			input := &dynamodb.ScanInput{
				TableName:              aws.String(tableName),
				ProjectionExpression:   aws.String(projection),
				Segment:                aws.Int64(int64(segment)),
				TotalSegments:          aws.Int64(int64(len(processors))),
				ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
			}
			withRetrys := func(req *request.Request) {
				req.Retryer = client.DefaultRetryer{NumMaxRetries: r.maxRetries}
			}
			err := r.DynamoDB.ScanPagesWithContext(ctx, input, func(page *dynamodb.ScanOutput, _ bool) bool {
				if cc := page.ConsumedCapacity; cc != nil {
					r.metrics.dynamoConsumedCapacity.WithLabelValues("DynamoDB.ScanTable", *cc.TableName).
						Add(*cc.CapacityUnits)
				}
				r.processPage(ctx, sm, processor, tableName, page)
				return true
			}, withRetrys)
			if err != nil {
				return err
			}
			processor.Flush()
			level.Info(r.log).Log("msg", "Segment finished", "segment", segment)
			return nil
		})
	}
	// Wait until all reader segments have finished
	outerErr := readerGroup.Wait()
	if outerErr != nil {
		return outerErr
	}
	return nil
}

func (r *dynamodbIndexReader) processPage(ctx context.Context, sm *seriesMap, processor index.EntryProcessor, tableName string, page *dynamodb.ScanOutput) {
	for _, item := range page.Items {
		r.rowsRead.Inc()
		rangeValue := item[rangeKey].B
		if !isSeriesIndexEntry(rangeValue) {
			continue
		}
		hashValue := aws.StringValue(item[hashKey].S)
		orgStr, day, seriesID, err := decodeHashValue(hashValue)
		if err != nil {
			level.Error(r.log).Log("msg", "Failed to decode hash value", "err", err)
			continue
		}
		if !processor.AcceptUser(orgStr) {
			continue
		}

		bucketHashKey := orgStr + ":" + day // from v9Entries.GetChunkWriteEntries()

		// Check whether we have already processed this series
		// via two-step lookup: first by tenant/day bucket, then by series
		var seriesSha256 sha256
		err = decodeBase64(seriesSha256[:], seriesID)
		if err != nil {
			level.Error(r.log).Log("msg", "Failed to decode series ID", "err", err)
			continue
		}
		sm.mutex.Lock()
		shaSet := sm.seriesProcessed[bucketHashKey]
		if shaSet.series == nil {
			shaSet.series = make(map[sha256]struct{})
			sm.seriesProcessed[bucketHashKey] = shaSet
		}
		if _, exists := shaSet.series[seriesSha256]; exists {
			sm.mutex.Unlock()
			continue
		}
		// mark it as 'seen already'
		shaSet.series[seriesSha256] = struct{}{}
		sm.mutex.Unlock()

		err = r.queryChunkEntriesForSeries(ctx, processor, tableName, bucketHashKey+":"+seriesID)
		if err != nil {
			level.Error(r.log).Log("msg", "error while reading series", "err", err)
			return
		}
	}
}

func decodeBase64(dst []byte, value string) error {
	n, err := base64.RawStdEncoding.Decode(dst, []byte(value))
	if err != nil {
		return errors.Wrap(err, "unable to decode sha256")
	}
	if n != len(dst) {
		return errors.Wrapf(err, "seriesID has unexpected length; raw value %q", value)
	}
	return nil
}

func (r *dynamodbIndexReader) queryChunkEntriesForSeries(ctx context.Context, processor index.EntryProcessor, tableName, queryHashKey string) error {
	// DynamoDB query which just says "all rows with hashKey X"
	// This is hard-coded for schema v9
	input := &dynamodb.QueryInput{
		TableName: aws.String(tableName),
		KeyConditions: map[string]*dynamodb.Condition{
			hashKey: {
				AttributeValueList: []*dynamodb.AttributeValue{
					{S: aws.String(queryHashKey)},
				},
				ComparisonOperator: aws.String(dynamodb.ComparisonOperatorEq),
			},
		},
		ReturnConsumedCapacity: aws.String(dynamodb.ReturnConsumedCapacityTotal),
	}
	withRetrys := func(req *request.Request) {
		req.Retryer = client.DefaultRetryer{NumMaxRetries: r.maxRetries}
	}
	var result error
	err := r.DynamoDB.QueryPagesWithContext(ctx, input, func(output *dynamodb.QueryOutput, _ bool) bool {
		if cc := output.ConsumedCapacity; cc != nil {
			r.metrics.dynamoConsumedCapacity.WithLabelValues("DynamoDB.QueryPages", *cc.TableName).
				Add(*cc.CapacityUnits)
		}

		for _, item := range output.Items {
			err := processor.ProcessIndexEntry(index.Entry{
				TableName:  tableName,
				HashValue:  aws.StringValue(item[hashKey].S),
				RangeValue: item[rangeKey].B,
			})
			if err != nil {
				result = errors.Wrap(err, "processor error")
				return false
			}
		}
		return true
	}, withRetrys)
	if err != nil {
		return errors.Wrap(err, "DynamoDB error")
	}
	return result
}

func isSeriesIndexEntry(rangeValue []byte) bool {
	const chunkTimeRangeKeyV3 = '3' // copied from pkg/chunk/schema.go
	return len(rangeValue) > 2 && rangeValue[len(rangeValue)-2] == chunkTimeRangeKeyV3
}

func decodeHashValue(hashValue string) (orgStr, day, seriesID string, err error) {
	hashParts := strings.SplitN(hashValue, ":", 3)
	if len(hashParts) != 3 {
		err = fmt.Errorf("unrecognized hash value: %q", hashValue)
		return
	}
	orgStr = hashParts[0]
	day = hashParts[1]
	seriesID = hashParts[2]
	return
}

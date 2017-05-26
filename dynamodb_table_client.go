package chunk

import (
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
	"github.com/prometheus/common/log"
	"golang.org/x/net/context"
	"golang.org/x/time/rate"

	"github.com/weaveworks/common/instrument"
)

type dynamoTableClient struct {
	DynamoDB dynamodbiface.DynamoDBAPI
	limiter  *rate.Limiter
}

// NewDynamoDBTableClient makes a new DynamoTableClient.
func NewDynamoDBTableClient(cfg DynamoDBConfig) (TableClient, error) {
	dynamoDB, err := dynamoClientFromURL(cfg.DynamoDB.URL)
	if err != nil {
		return nil, err
	}
	return dynamoTableClient{
		DynamoDB: dynamoDB,
		limiter:  rate.NewLimiter(rate.Limit(cfg.APILimit), 1),
	}, nil
}

func (d dynamoTableClient) backoffAndRetry(ctx context.Context, fn func(context.Context) error) error {
	if d.limiter != nil { // Tests will have a nil limiter.
		d.limiter.Wait(ctx)
	}

	backoff, numRetries := minBackoff, 10
	for i := 0; i < numRetries; i++ {
		if err := fn(ctx); err != nil {
			if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == "ThrottlingException" {
				log.Errorf("Got error %v on try %d, backing off and retrying.", err, i)
				time.Sleep(backoff)
				backoff = nextBackoff(backoff)
				continue
			} else {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("retried %d times, failing", numRetries)
}

func (d dynamoTableClient) ListTables(ctx context.Context) ([]string, error) {
	table := []string{}
	err := d.backoffAndRetry(ctx, func(ctx context.Context) error {
		return instrument.TimeRequestHistogram(ctx, "DynamoDB.ListTablesPages", dynamoRequestDuration, func(ctx context.Context) error {
			return d.DynamoDB.ListTablesPagesWithContext(ctx, &dynamodb.ListTablesInput{}, func(resp *dynamodb.ListTablesOutput, _ bool) bool {
				for _, s := range resp.TableNames {
					table = append(table, *s)
				}
				return true
			})
		})
	})
	return table, err
}

func (d dynamoTableClient) CreateTable(ctx context.Context, desc TableDesc) error {
	var tableARN *string
	if err := d.backoffAndRetry(ctx, func(ctx context.Context) error {
		return instrument.TimeRequestHistogram(ctx, "DynamoDB.CreateTable", dynamoRequestDuration, func(ctx context.Context) error {
			input := &dynamodb.CreateTableInput{
				TableName: aws.String(desc.Name),
				AttributeDefinitions: []*dynamodb.AttributeDefinition{
					{
						AttributeName: aws.String(hashKey),
						AttributeType: aws.String(dynamodb.ScalarAttributeTypeS),
					},
					{
						AttributeName: aws.String(rangeKey),
						AttributeType: aws.String(dynamodb.ScalarAttributeTypeB),
					},
				},
				KeySchema: []*dynamodb.KeySchemaElement{
					{
						AttributeName: aws.String(hashKey),
						KeyType:       aws.String(dynamodb.KeyTypeHash),
					},
					{
						AttributeName: aws.String(rangeKey),
						KeyType:       aws.String(dynamodb.KeyTypeRange),
					},
				},
				ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
					ReadCapacityUnits:  aws.Int64(desc.ProvisionedRead),
					WriteCapacityUnits: aws.Int64(desc.ProvisionedWrite),
				},
			}
			output, err := d.DynamoDB.CreateTableWithContext(ctx, input)
			if err != nil {
				return err
			}
			tableARN = output.TableDescription.TableArn
			return nil
		})
	}); err != nil {
		return err
	}

	tags := desc.Tags.AWSTags()
	if len(tags) > 0 {
		return d.backoffAndRetry(ctx, func(ctx context.Context) error {
			return instrument.TimeRequestHistogram(ctx, "DynamoDB.TagResource", dynamoRequestDuration, func(ctx context.Context) error {
				_, err := d.DynamoDB.TagResourceWithContext(ctx, &dynamodb.TagResourceInput{
					ResourceArn: tableARN,
					Tags:        tags,
				})
				return err
			})
		})
	}
	return nil
}

func (d dynamoTableClient) DescribeTable(ctx context.Context, name string) (desc TableDesc, status string, err error) {
	var tableARN *string
	err = d.backoffAndRetry(ctx, func(ctx context.Context) error {
		return instrument.TimeRequestHistogram(ctx, "DynamoDB.DescribeTable", dynamoRequestDuration, func(ctx context.Context) error {
			out, err := d.DynamoDB.DescribeTableWithContext(ctx, &dynamodb.DescribeTableInput{
				TableName: aws.String(name),
			})
			desc.Name = name
			desc.ProvisionedRead = *out.Table.ProvisionedThroughput.ReadCapacityUnits
			desc.ProvisionedWrite = *out.Table.ProvisionedThroughput.WriteCapacityUnits
			status = *out.Table.TableStatus
			tableARN = out.Table.TableArn
			return err
		})
	})
	if err != nil {
		return
	}

	err = d.backoffAndRetry(ctx, func(ctx context.Context) error {
		return instrument.TimeRequestHistogram(ctx, "DynamoDB.ListTagsOfResource", dynamoRequestDuration, func(ctx context.Context) error {
			out, err := d.DynamoDB.ListTagsOfResourceWithContext(ctx, &dynamodb.ListTagsOfResourceInput{
				ResourceArn: tableARN,
			})
			desc.Tags = make(map[string]string, len(out.Tags))
			for _, tag := range out.Tags {
				desc.Tags[*tag.Key] = *tag.Value
			}
			return err
		})
	})
	return
}

func (d dynamoTableClient) UpdateTable(ctx context.Context, current, expected TableDesc) error {

	if current.ProvisionedRead != expected.ProvisionedRead || current.ProvisionedWrite != expected.ProvisionedWrite {
		if err := d.backoffAndRetry(ctx, func(ctx context.Context) error {
			return instrument.TimeRequestHistogram(ctx, "DynamoDB.UpdateTable", dynamoRequestDuration, func(ctx context.Context) error {
				_, err := d.DynamoDB.UpdateTableWithContext(ctx, &dynamodb.UpdateTableInput{
					TableName: aws.String(expected.Name),
					ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
						ReadCapacityUnits:  aws.Int64(expected.ProvisionedRead),
						WriteCapacityUnits: aws.Int64(expected.ProvisionedWrite),
					},
				})
				return err
			})
		}); err != nil {
			return err
		}
	}

	if !current.Tags.Equals(expected.Tags) {
		var tableARN *string
		if err := d.backoffAndRetry(ctx, func(ctx context.Context) error {
			return instrument.TimeRequestHistogram(ctx, "DynamoDB.DescribeTable", dynamoRequestDuration, func(ctx context.Context) error {
				out, err := d.DynamoDB.DescribeTableWithContext(ctx, &dynamodb.DescribeTableInput{
					TableName: aws.String(expected.Name),
				})
				if err != nil {
					return err
				}
				tableARN = out.Table.TableArn
				return nil
			})
		}); err != nil {
			return err
		}

		return d.backoffAndRetry(ctx, func(ctx context.Context) error {
			return instrument.TimeRequestHistogram(ctx, "DynamoDB.TagResource", dynamoRequestDuration, func(ctx context.Context) error {
				_, err := d.DynamoDB.TagResourceWithContext(ctx, &dynamodb.TagResourceInput{
					ResourceArn: tableARN,
					Tags:        expected.Tags.AWSTags(),
				})
				return err
			})
		})
	}
	return nil
}

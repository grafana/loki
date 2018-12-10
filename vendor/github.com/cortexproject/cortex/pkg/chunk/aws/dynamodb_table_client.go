package aws

import (
	"context"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
	"github.com/go-kit/kit/log/level"
	"golang.org/x/time/rate"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/weaveworks/common/instrument"
)

// Pluggable auto-scaler implementation
type autoscale interface {
	PostCreateTable(ctx context.Context, desc chunk.TableDesc) error
	// This whole interface is very similar to chunk.TableClient, but
	// DescribeTable needs to mutate desc
	DescribeTable(ctx context.Context, desc *chunk.TableDesc) error
	UpdateTable(ctx context.Context, current chunk.TableDesc, expected *chunk.TableDesc) error
}

type callManager struct {
	limiter       *rate.Limiter
	backoffConfig util.BackoffConfig
}

type dynamoTableClient struct {
	DynamoDB    dynamodbiface.DynamoDBAPI
	callManager callManager
	autoscale   autoscale
}

// NewDynamoDBTableClient makes a new DynamoTableClient.
func NewDynamoDBTableClient(cfg DynamoDBConfig) (chunk.TableClient, error) {
	dynamoDB, err := dynamoClientFromURL(cfg.DynamoDB.URL)
	if err != nil {
		return nil, err
	}

	callManager := callManager{
		limiter:       rate.NewLimiter(rate.Limit(cfg.APILimit), 1),
		backoffConfig: cfg.backoffConfig,
	}

	var autoscale autoscale
	if cfg.ApplicationAutoScaling.URL != nil {
		autoscale, err = newAWSAutoscale(cfg, callManager)
		if err != nil {
			return nil, err
		}
	}

	if cfg.Metrics.URL != "" {
		autoscale, err = newMetrics(cfg)
		if err != nil {
			return nil, err
		}
	}

	return dynamoTableClient{
		DynamoDB:    dynamoDB,
		callManager: callManager,
		autoscale:   autoscale,
	}, nil
}

func (d *dynamoTableClient) Stop() {
}

func (d dynamoTableClient) backoffAndRetry(ctx context.Context, fn func(context.Context) error) error {
	return d.callManager.backoffAndRetry(ctx, fn)
}

func (d callManager) backoffAndRetry(ctx context.Context, fn func(context.Context) error) error {
	if d.limiter != nil { // Tests will have a nil limiter.
		d.limiter.Wait(ctx)
	}

	backoff := util.NewBackoff(ctx, d.backoffConfig)
	for backoff.Ongoing() {
		if err := fn(ctx); err != nil {
			if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == "ThrottlingException" {
				level.Warn(util.WithContext(ctx, util.Logger)).Log("msg", "got error, backing off and retrying", "err", err, "retry", backoff.NumRetries())
				backoff.Wait()
				continue
			} else {
				return err
			}
		}
		return nil
	}
	return backoff.Err()
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

func chunkTagsToDynamoDB(ts chunk.Tags) []*dynamodb.Tag {
	var result []*dynamodb.Tag
	for k, v := range ts {
		result = append(result, &dynamodb.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}
	return result
}

func (d dynamoTableClient) CreateTable(ctx context.Context, desc chunk.TableDesc) error {
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
			if output.TableDescription != nil {
				tableARN = output.TableDescription.TableArn
			}
			return nil
		})
	}); err != nil {
		return err
	}

	if d.autoscale != nil {
		err := d.autoscale.PostCreateTable(ctx, desc)
		if err != nil {
			return err
		}
	}

	tags := chunkTagsToDynamoDB(desc.Tags)
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

func (d dynamoTableClient) DescribeTable(ctx context.Context, name string) (desc chunk.TableDesc, isActive bool, err error) {
	var tableARN *string
	err = d.backoffAndRetry(ctx, func(ctx context.Context) error {
		return instrument.TimeRequestHistogram(ctx, "DynamoDB.DescribeTable", dynamoRequestDuration, func(ctx context.Context) error {
			out, err := d.DynamoDB.DescribeTableWithContext(ctx, &dynamodb.DescribeTableInput{
				TableName: aws.String(name),
			})
			if err != nil {
				return err
			}
			desc.Name = name
			if out.Table != nil {
				if provision := out.Table.ProvisionedThroughput; provision != nil {
					if provision.ReadCapacityUnits != nil {
						desc.ProvisionedRead = *provision.ReadCapacityUnits
					}
					if provision.WriteCapacityUnits != nil {
						desc.ProvisionedWrite = *provision.WriteCapacityUnits
					}
				}
				if out.Table.TableStatus != nil {
					isActive = (*out.Table.TableStatus == dynamodb.TableStatusActive)
				}
				tableARN = out.Table.TableArn
			}
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
			if err != nil {
				return err
			}
			desc.Tags = make(map[string]string, len(out.Tags))
			for _, tag := range out.Tags {
				desc.Tags[*tag.Key] = *tag.Value
			}
			return err
		})
	})

	if d.autoscale != nil {
		err = d.autoscale.DescribeTable(ctx, &desc)
	}
	return
}

func (d dynamoTableClient) UpdateTable(ctx context.Context, current, expected chunk.TableDesc) error {
	if d.autoscale != nil {
		err := d.autoscale.UpdateTable(ctx, current, &expected)
		if err != nil {
			return err
		}
	}

	if current.ProvisionedRead != expected.ProvisionedRead || current.ProvisionedWrite != expected.ProvisionedWrite {
		level.Info(util.Logger).Log("msg", "updating provisioned throughput on table", "table", expected.Name, "old_read", current.ProvisionedRead, "old_write", current.ProvisionedWrite, "new_read", expected.ProvisionedRead, "new_write", expected.ProvisionedWrite)
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
			recordDynamoError(expected.Name, err, "DynamoDB.UpdateTable")
			if awsErr, ok := err.(awserr.Error); ok && awsErr.Code() == "LimitExceededException" {
				level.Warn(util.Logger).Log("msg", "update limit exceeded", "err", err)
			} else {
				return err
			}
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
				if out.Table != nil {
					tableARN = out.Table.TableArn
				}
				return nil
			})
		}); err != nil {
			return err
		}

		return d.backoffAndRetry(ctx, func(ctx context.Context) error {
			return instrument.TimeRequestHistogram(ctx, "DynamoDB.TagResource", dynamoRequestDuration, func(ctx context.Context) error {
				_, err := d.DynamoDB.TagResourceWithContext(ctx, &dynamodb.TagResourceInput{
					ResourceArn: tableARN,
					Tags:        chunkTagsToDynamoDB(expected.Tags),
				})
				return err
			})
		})
	}
	return nil
}

package chunk

import (
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/aws/aws-sdk-go/service/dynamodb/dynamodbiface"
	"golang.org/x/net/context"

	"github.com/weaveworks/common/instrument"
)

type dynamoTableClient struct {
	DynamoDB dynamodbiface.DynamoDBAPI
}

// NewDynamoDBTableClient makes a new DynamoTableClient.
func NewDynamoDBTableClient(cfg DynamoDBConfig) (TableClient, error) {
	dynamoDB, err := dynamoClientFromURL(cfg.DynamoDB.URL)
	if err != nil {
		return nil, err
	}
	return dynamoTableClient{
		DynamoDB: dynamoDB,
	}, nil
}

func (d dynamoTableClient) ListTables(ctx context.Context) ([]string, error) {
	table := []string{}
	err := instrument.TimeRequestHistogram(ctx, "DynamoDB.ListTablesPages", dynamoRequestDuration, func(ctx context.Context) error {
		return d.DynamoDB.ListTablesPagesWithContext(ctx, &dynamodb.ListTablesInput{}, func(resp *dynamodb.ListTablesOutput, _ bool) bool {
			for _, s := range resp.TableNames {
				table = append(table, *s)
			}
			return true
		})
	})
	return table, err
}

func (d dynamoTableClient) CreateTable(ctx context.Context, desc TableDesc) error {
	var tableARN *string
	if err := instrument.TimeRequestHistogram(ctx, "DynamoDB.CreateTable", dynamoRequestDuration, func(ctx context.Context) error {
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
	}); err != nil {
		return err
	}

	return instrument.TimeRequestHistogram(ctx, "DynamoDB.TagResource", dynamoRequestDuration, func(ctx context.Context) error {
		_, err := d.DynamoDB.TagResourceWithContext(ctx, &dynamodb.TagResourceInput{
			ResourceArn: tableARN,
			Tags:        desc.Tags.AWSTags(),
		})
		return err
	})
}

func (d dynamoTableClient) DescribeTable(ctx context.Context, name string) (desc TableDesc, status string, err error) {
	var tableARN *string
	err = instrument.TimeRequestHistogram(ctx, "DynamoDB.DescribeTable", dynamoRequestDuration, func(ctx context.Context) error {
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
	if err != nil {
		return
	}

	err = instrument.TimeRequestHistogram(ctx, "DynamoDB.ListTagsOfResource", dynamoRequestDuration, func(ctx context.Context) error {
		out, err := d.DynamoDB.ListTagsOfResourceWithContext(ctx, &dynamodb.ListTagsOfResourceInput{
			ResourceArn: tableARN,
		})
		desc.Tags = make(map[string]string, len(out.Tags))
		for _, tag := range out.Tags {
			desc.Tags[*tag.Key] = *tag.Value
		}
		return err
	})
	return
}

func (d dynamoTableClient) UpdateTable(ctx context.Context, desc TableDesc) error {
	var tableARN *string
	if err := instrument.TimeRequestHistogram(ctx, "DynamoDB.UpdateTable", dynamoRequestDuration, func(ctx context.Context) error {
		out, err := d.DynamoDB.UpdateTableWithContext(ctx, &dynamodb.UpdateTableInput{
			TableName: aws.String(desc.Name),
			ProvisionedThroughput: &dynamodb.ProvisionedThroughput{
				ReadCapacityUnits:  aws.Int64(desc.ProvisionedRead),
				WriteCapacityUnits: aws.Int64(desc.ProvisionedWrite),
			},
		})
		if err != nil {
			return err
		}
		tableARN = out.TableDescription.TableArn
		return nil
	}); err != nil {
		return err
	}

	return instrument.TimeRequestHistogram(ctx, "DynamoDB.TagResource", dynamoRequestDuration, func(ctx context.Context) error {
		_, err := d.DynamoDB.TagResourceWithContext(ctx, &dynamodb.TagResourceInput{
			ResourceArn: tableARN,
			Tags:        desc.Tags.AWSTags(),
		})
		return err
	})
}

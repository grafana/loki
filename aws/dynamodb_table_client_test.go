package aws

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/applicationautoscaling"
	"github.com/aws/aws-sdk-go/service/applicationautoscaling/applicationautoscalingiface"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/mtime"

	"github.com/cortexproject/cortex/pkg/chunk"
)

const (
	tablePrefix      = "cortex_"
	chunkTablePrefix = "chunks_"
	tablePeriod      = 7 * 24 * time.Hour
	gracePeriod      = 15 * time.Minute
	maxChunkAge      = 12 * time.Hour
	inactiveWrite    = 1
	inactiveRead     = 2
	write            = 200
	read             = 100
)

func fixtureWriteScale() chunk.AutoScalingConfig {
	return chunk.AutoScalingConfig{
		Enabled:     true,
		MinCapacity: 100,
		MaxCapacity: 250,
		OutCooldown: 100,
		InCooldown:  100,
		TargetValue: 80.0,
	}
}

func fixtureReadScale() chunk.AutoScalingConfig {
	return chunk.AutoScalingConfig{
		Enabled:     true,
		MinCapacity: 1,
		MaxCapacity: 2000,
		OutCooldown: 100,
		InCooldown:  100,
		TargetValue: 80.0,
	}
}

func fixturePeriodicTableConfig(prefix string) chunk.PeriodicTableConfig {
	return chunk.PeriodicTableConfig{
		Prefix: prefix,
		Period: tablePeriod,
	}
}

func fixtureProvisionConfig(inactLastN int64, writeScale, inactWriteScale chunk.AutoScalingConfig) chunk.ProvisionConfig {
	return chunk.ProvisionConfig{
		ProvisionedWriteThroughput: write,
		ProvisionedReadThroughput:  read,
		InactiveWriteThroughput:    inactiveWrite,
		InactiveReadThroughput:     inactiveRead,
		WriteScale:                 writeScale,
		InactiveWriteScale:         inactWriteScale,
		InactiveWriteScaleLastN:    inactLastN,
	}
}

func fixtureReadProvisionConfig(readScale, inactReadScale chunk.AutoScalingConfig) chunk.ProvisionConfig {
	return chunk.ProvisionConfig{
		ProvisionedWriteThroughput: write,
		ProvisionedReadThroughput:  read,
		InactiveWriteThroughput:    inactiveWrite,
		InactiveReadThroughput:     inactiveRead,
		ReadScale:                  readScale,
		InactiveReadScale:          inactReadScale,
	}
}

func baseTable(name string, provisionedRead, provisionedWrite int64) []chunk.TableDesc {
	return []chunk.TableDesc{
		{
			Name:             name,
			ProvisionedRead:  provisionedRead,
			ProvisionedWrite: provisionedWrite,
		},
	}
}

func staticTable(i int, indexRead, indexWrite, chunkRead, chunkWrite int64) []chunk.TableDesc {
	return []chunk.TableDesc{
		{
			Name:             tablePrefix + fmt.Sprint(i),
			ProvisionedRead:  indexRead,
			ProvisionedWrite: indexWrite,
		},
		{
			Name:             chunkTablePrefix + fmt.Sprint(i),
			ProvisionedRead:  chunkRead,
			ProvisionedWrite: chunkWrite,
		},
	}
}

func autoScaledTable(i int, provisionedRead, provisionedWrite int64, indexOutCooldown int64, chunkTarget float64) []chunk.TableDesc {
	chunkASC, indexASC := fixtureWriteScale(), fixtureWriteScale()
	indexASC.OutCooldown = indexOutCooldown
	chunkASC.TargetValue = chunkTarget
	return []chunk.TableDesc{
		{
			Name:             tablePrefix + fmt.Sprint(i),
			ProvisionedRead:  provisionedRead,
			ProvisionedWrite: provisionedWrite,
			WriteScale:       indexASC,
		},
		{
			Name:             chunkTablePrefix + fmt.Sprint(i),
			ProvisionedRead:  provisionedRead,
			ProvisionedWrite: provisionedWrite,
			WriteScale:       chunkASC,
		},
	}
}

func test(t *testing.T, client dynamoTableClient, tableManager *chunk.TableManager, name string, tm time.Time, expected []chunk.TableDesc) {
	t.Run(name, func(t *testing.T) {
		ctx := context.Background()
		mtime.NowForce(tm)
		defer mtime.NowReset()
		if err := tableManager.SyncTables(ctx); err != nil {
			t.Fatal(err)
		}
		err := chunk.ExpectTables(ctx, client, expected)
		require.NoError(t, err)
	})
}

func TestTableManagerAutoScaling(t *testing.T) {
	dynamoDB := newMockDynamoDB(0, 0)
	applicationAutoScaling := newMockApplicationAutoScaling()
	client := dynamoTableClient{
		DynamoDB:  dynamoDB,
		autoscale: &awsAutoscale{ApplicationAutoScaling: applicationAutoScaling},
	}

	cfg := chunk.SchemaConfig{
		Configs: []chunk.PeriodConfig{
			{
				IndexType: "aws-dynamo",
			},
			{
				IndexType:   "aws-dynamo",
				From:        chunk.DayTime{Time: model.TimeFromUnix(0)},
				IndexTables: fixturePeriodicTableConfig(tablePrefix),
				ChunkTables: fixturePeriodicTableConfig(chunkTablePrefix),
			}},
	}
	tbm := chunk.TableManagerConfig{
		CreationGracePeriod: gracePeriod,
		IndexTables:         fixtureProvisionConfig(0, fixtureWriteScale(), chunk.AutoScalingConfig{}),
		ChunkTables:         fixtureProvisionConfig(0, fixtureWriteScale(), chunk.AutoScalingConfig{}),
	}

	// Check tables are created with autoscale
	{
		tableManager, err := chunk.NewTableManager(tbm, cfg, maxChunkAge, client, nil)
		if err != nil {
			t.Fatal(err)
		}

		test(t, client,
			tableManager,
			"Create tables",
			time.Unix(0, 0).Add(maxChunkAge).Add(gracePeriod),
			append(baseTable("", inactiveRead, inactiveWrite),
				autoScaledTable(0, read, write, 100, 80)...),
		)
	}

	// Check tables are updated with new settings
	{
		tbm.IndexTables.WriteScale.OutCooldown = 200
		tbm.ChunkTables.WriteScale.TargetValue = 90.0

		tableManager, err := chunk.NewTableManager(tbm, cfg, maxChunkAge, client, nil)
		if err != nil {
			t.Fatal(err)
		}

		test(t, client,
			tableManager,
			"Update tables with new settings",
			time.Unix(0, 0).Add(maxChunkAge).Add(gracePeriod),
			append(baseTable("", inactiveRead, inactiveWrite),
				autoScaledTable(0, read, write, 200, 90)...),
		)
	}

	// Check tables are degristered when autoscaling is disabled for inactive tables
	{
		tbm.IndexTables.WriteScale.OutCooldown = 200
		tbm.ChunkTables.WriteScale.TargetValue = 90.0

		tableManager, err := chunk.NewTableManager(tbm, cfg, maxChunkAge, client, nil)
		if err != nil {
			t.Fatal(err)
		}

		test(t, client,
			tableManager,
			"Update tables with new settings",
			time.Unix(0, 0).Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
			append(append(baseTable("", inactiveRead, inactiveWrite),
				staticTable(0, inactiveRead, inactiveWrite, inactiveRead, inactiveWrite)...),
				autoScaledTable(1, read, write, 200, 90)...),
		)
	}

	// Check tables are degristered when autoscaling is disabled entirely
	{
		tbm.IndexTables.WriteScale.Enabled = false
		tbm.ChunkTables.WriteScale.Enabled = false

		tableManager, err := chunk.NewTableManager(tbm, cfg, maxChunkAge, client, nil)
		if err != nil {
			t.Fatal(err)
		}

		test(t, client,
			tableManager,
			"Update tables with new settings",
			time.Unix(0, 0).Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
			append(append(baseTable("", inactiveRead, inactiveWrite),
				staticTable(0, inactiveRead, inactiveWrite, inactiveRead, inactiveWrite)...),
				staticTable(1, read, write, read, write)...),
		)
	}
}

func TestTableManagerInactiveAutoScaling(t *testing.T) {
	dynamoDB := newMockDynamoDB(0, 0)
	applicationAutoScaling := newMockApplicationAutoScaling()
	client := dynamoTableClient{
		DynamoDB:  dynamoDB,
		autoscale: &awsAutoscale{ApplicationAutoScaling: applicationAutoScaling},
	}

	cfg := chunk.SchemaConfig{
		Configs: []chunk.PeriodConfig{
			{
				IndexType:   "aws-dynamo",
				IndexTables: chunk.PeriodicTableConfig{},
			},
			{
				IndexType:   "aws-dynamo",
				IndexTables: fixturePeriodicTableConfig(tablePrefix),
				ChunkTables: fixturePeriodicTableConfig(chunkTablePrefix),
			},
		},
	}
	tbm := chunk.TableManagerConfig{
		CreationGracePeriod: gracePeriod,
		IndexTables:         fixtureProvisionConfig(2, chunk.AutoScalingConfig{}, fixtureWriteScale()),
		ChunkTables:         fixtureProvisionConfig(2, chunk.AutoScalingConfig{}, fixtureWriteScale()),
	}

	// Check legacy and latest tables do not autoscale with inactive autoscale enabled.
	{
		tableManager, err := chunk.NewTableManager(tbm, cfg, maxChunkAge, client, nil)
		if err != nil {
			t.Fatal(err)
		}

		test(t, client,
			tableManager,
			"Legacy and latest tables",
			time.Unix(0, 0).Add(maxChunkAge).Add(gracePeriod),
			append(baseTable("", inactiveRead, inactiveWrite),
				staticTable(0, read, write, read, write)...),
		)
	}

	// Check inactive tables are autoscaled even if there are less than the limit.
	{
		tableManager, err := chunk.NewTableManager(tbm, cfg, maxChunkAge, client, nil)
		if err != nil {
			t.Fatal(err)
		}

		test(t, client,
			tableManager,
			"1 week of inactive tables with latest",
			time.Unix(0, 0).Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
			append(append(baseTable("", inactiveRead, inactiveWrite),
				autoScaledTable(0, inactiveRead, inactiveWrite, 100, 80)...),
				staticTable(1, read, write, read, write)...),
		)
	}

	// Check inactive tables past the limit do not autoscale but the latest N do.
	{
		tableManager, err := chunk.NewTableManager(tbm, cfg, maxChunkAge, client, nil)
		if err != nil {
			t.Fatal(err)
		}

		test(t, client,
			tableManager,
			"3 weeks of inactive tables with latest",
			time.Unix(0, 0).Add(tablePeriod*3).Add(maxChunkAge).Add(gracePeriod),
			append(append(append(append(baseTable("", inactiveRead, inactiveWrite),
				staticTable(0, inactiveRead, inactiveWrite, inactiveRead, inactiveWrite)...),
				autoScaledTable(1, inactiveRead, inactiveWrite, 100, 80)...),
				autoScaledTable(2, inactiveRead, inactiveWrite, 100, 80)...),
				staticTable(3, read, write, read, write)...),
		)
	}
}

type mockApplicationAutoScalingClient struct {
	applicationautoscalingiface.ApplicationAutoScalingAPI

	scalableTargets map[string]mockScalableTarget
	scalingPolicies map[string]mockScalingPolicy
}

type mockScalableTarget struct {
	RoleARN     string
	MinCapacity int64
	MaxCapacity int64
}

type mockScalingPolicy struct {
	ScaleInCooldown  int64
	ScaleOutCooldown int64
	TargetValue      float64
}

func newMockApplicationAutoScaling() *mockApplicationAutoScalingClient {
	return &mockApplicationAutoScalingClient{
		scalableTargets: map[string]mockScalableTarget{},
		scalingPolicies: map[string]mockScalingPolicy{},
	}
}

func (m *mockApplicationAutoScalingClient) RegisterScalableTarget(input *applicationautoscaling.RegisterScalableTargetInput) (*applicationautoscaling.RegisterScalableTargetOutput, error) {
	m.scalableTargets[*input.ResourceId] = mockScalableTarget{
		RoleARN:     *input.RoleARN,
		MinCapacity: *input.MinCapacity,
		MaxCapacity: *input.MaxCapacity,
	}
	return &applicationautoscaling.RegisterScalableTargetOutput{}, nil
}

func (m *mockApplicationAutoScalingClient) DeregisterScalableTarget(input *applicationautoscaling.DeregisterScalableTargetInput) (*applicationautoscaling.DeregisterScalableTargetOutput, error) {
	delete(m.scalableTargets, *input.ResourceId)
	return &applicationautoscaling.DeregisterScalableTargetOutput{}, nil
}

func (m *mockApplicationAutoScalingClient) DescribeScalableTargetsWithContext(ctx aws.Context, input *applicationautoscaling.DescribeScalableTargetsInput, options ...request.Option) (*applicationautoscaling.DescribeScalableTargetsOutput, error) {
	scalableTarget, ok := m.scalableTargets[*input.ResourceIds[0]]
	if !ok {
		return &applicationautoscaling.DescribeScalableTargetsOutput{}, nil
	}
	return &applicationautoscaling.DescribeScalableTargetsOutput{
		ScalableTargets: []*applicationautoscaling.ScalableTarget{
			{
				RoleARN:     aws.String(scalableTarget.RoleARN),
				MinCapacity: aws.Int64(scalableTarget.MinCapacity),
				MaxCapacity: aws.Int64(scalableTarget.MaxCapacity),
			},
		},
	}, nil
}

func (m *mockApplicationAutoScalingClient) PutScalingPolicy(input *applicationautoscaling.PutScalingPolicyInput) (*applicationautoscaling.PutScalingPolicyOutput, error) {
	m.scalingPolicies[*input.ResourceId] = mockScalingPolicy{
		ScaleInCooldown:  *input.TargetTrackingScalingPolicyConfiguration.ScaleInCooldown,
		ScaleOutCooldown: *input.TargetTrackingScalingPolicyConfiguration.ScaleOutCooldown,
		TargetValue:      *input.TargetTrackingScalingPolicyConfiguration.TargetValue,
	}
	return &applicationautoscaling.PutScalingPolicyOutput{}, nil
}

func (m *mockApplicationAutoScalingClient) DeleteScalingPolicy(input *applicationautoscaling.DeleteScalingPolicyInput) (*applicationautoscaling.DeleteScalingPolicyOutput, error) {
	delete(m.scalingPolicies, *input.ResourceId)
	return &applicationautoscaling.DeleteScalingPolicyOutput{}, nil
}

func (m *mockApplicationAutoScalingClient) DescribeScalingPoliciesWithContext(ctx aws.Context, input *applicationautoscaling.DescribeScalingPoliciesInput, options ...request.Option) (*applicationautoscaling.DescribeScalingPoliciesOutput, error) {
	scalingPolicy, ok := m.scalingPolicies[*input.ResourceId]
	if !ok {
		return &applicationautoscaling.DescribeScalingPoliciesOutput{}, nil
	}
	return &applicationautoscaling.DescribeScalingPoliciesOutput{
		ScalingPolicies: []*applicationautoscaling.ScalingPolicy{
			{
				TargetTrackingScalingPolicyConfiguration: &applicationautoscaling.TargetTrackingScalingPolicyConfiguration{
					ScaleInCooldown:  aws.Int64(scalingPolicy.ScaleInCooldown),
					ScaleOutCooldown: aws.Int64(scalingPolicy.ScaleOutCooldown),
					TargetValue:      aws.Float64(scalingPolicy.TargetValue),
				},
			},
		},
	}, nil
}

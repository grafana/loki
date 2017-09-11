package chunk

import (
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/request"
	"github.com/aws/aws-sdk-go/service/applicationautoscaling"
	"github.com/aws/aws-sdk-go/service/applicationautoscaling/applicationautoscalingiface"
	"github.com/prometheus/common/model"
	"github.com/weaveworks/common/mtime"
	"golang.org/x/net/context"

	"github.com/weaveworks/cortex/pkg/util"
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

func TestTableManager(t *testing.T) {
	dynamoDB := newMockDynamoDB(0, 0)
	client := dynamoTableClient{
		DynamoDB: dynamoDB,
	}

	cfg := SchemaConfig{
		UsePeriodicTables: true,
		IndexTables: periodicTableConfig{
			Prefix: tablePrefix,
			Period: tablePeriod,
			From:   util.NewDayValue(model.TimeFromUnix(0)),
			ProvisionedWriteThroughput: write,
			ProvisionedReadThroughput:  read,
			InactiveWriteThroughput:    inactiveWrite,
			InactiveReadThroughput:     inactiveRead,
		},

		ChunkTables: periodicTableConfig{
			Prefix: chunkTablePrefix,
			Period: tablePeriod,
			From:   util.NewDayValue(model.TimeFromUnix(0)),
			ProvisionedWriteThroughput: write,
			ProvisionedReadThroughput:  read,
			InactiveWriteThroughput:    inactiveWrite,
			InactiveReadThroughput:     inactiveRead,
		},

		CreationGracePeriod: gracePeriod,
		MaxChunkAge:         maxChunkAge,
	}
	tableManager, err := NewTableManager(cfg, client)
	if err != nil {
		t.Fatal(err)
	}

	test := func(name string, tm time.Time, expected []TableDesc) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mtime.NowForce(tm)
			if err := tableManager.syncTables(ctx); err != nil {
				t.Fatal(err)
			}
			expectTables(ctx, t, client, expected)
		})
	}

	// Check at time zero, we have the base table and one weekly table
	test(
		"Initial test",
		time.Unix(0, 0),
		[]TableDesc{
			{Name: "", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Check running twice doesn't change anything
	test(
		"Nothing changed",
		time.Unix(0, 0),
		[]TableDesc{
			{Name: "", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward grace period, check we still have write throughput on base table
	test(
		"Move forward by grace period",
		time.Unix(0, 0).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward max chunk age + grace period, check write throughput on base table has gone
	test(
		"Move forward by max chunk age + grace period",
		time.Unix(0, 0).Add(maxChunkAge).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period - grace period, check we add another weekly table
	test(
		"Move forward by table period - grace period",
		time.Unix(0, 0).Add(tablePeriod).Add(-gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period + grace period, check we still have provisioned throughput
	test(
		"Move forward by table period + grace period",
		time.Unix(0, 0).Add(tablePeriod).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: tablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Fast forward table period + max chunk age + grace period, check we remove provisioned throughput
	test(
		"Move forward by table period + max chunk age + grace period",
		time.Unix(0, 0).Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)

	// Check running twice doesn't change anything
	test(
		"Nothing changed",
		time.Unix(0, 0).Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
		[]TableDesc{
			{Name: "", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: tablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
			{Name: chunkTablePrefix + "0", ProvisionedRead: inactiveRead, ProvisionedWrite: inactiveWrite},
			{Name: chunkTablePrefix + "1", ProvisionedRead: read, ProvisionedWrite: write},
		},
	)
}

func TestTableManagerTags(t *testing.T) {
	dynamoDB := newMockDynamoDB(0, 0)
	client := dynamoTableClient{
		DynamoDB: dynamoDB,
	}

	test := func(tableManager *TableManager, name string, tm time.Time, expected []TableDesc) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mtime.NowForce(tm)
			if err := tableManager.syncTables(ctx); err != nil {
				t.Fatal(err)
			}
			expectTables(ctx, t, client, expected)
		})
	}

	// Check at time zero, we have the base table with no tags.
	{
		tableManager, err := NewTableManager(SchemaConfig{}, client)
		if err != nil {
			t.Fatal(err)
		}

		test(
			tableManager,
			"Initial test",
			time.Unix(0, 0),
			[]TableDesc{
				{Name: ""},
			},
		)
	}

	// Check after restarting table manager we get some tags.
	{
		cfg := SchemaConfig{}
		cfg.IndexTables.Tags.Set("foo=bar")
		tableManager, err := NewTableManager(cfg, client)
		if err != nil {
			t.Fatal(err)
		}

		test(
			tableManager,
			"Tagged test",
			time.Unix(0, 0),
			[]TableDesc{
				{Name: "", Tags: Tags{"foo": "bar"}},
			},
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

func TestTableManagerAutoScaling(t *testing.T) {
	dynamoDB := newMockDynamoDB(0, 0)
	applicationAutoScaling := newMockApplicationAutoScaling()
	client := dynamoTableClient{
		DynamoDB:               dynamoDB,
		ApplicationAutoScaling: applicationAutoScaling,
	}

	test := func(tableManager *TableManager, name string, tm time.Time, expected []TableDesc) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mtime.NowForce(tm)
			if err := tableManager.syncTables(ctx); err != nil {
				t.Fatal(err)
			}
			expectTables(ctx, t, client, expected)
		})
	}

	cfg := SchemaConfig{
		UsePeriodicTables: true,
		IndexTables: periodicTableConfig{
			Prefix: tablePrefix,
			Period: tablePeriod,
			From:   util.NewDayValue(model.TimeFromUnix(0)),
			ProvisionedWriteThroughput: write,
			ProvisionedReadThroughput:  read,
			InactiveWriteThroughput:    inactiveWrite,
			InactiveReadThroughput:     inactiveRead,
			WriteScale: autoScalingConfig{
				Enabled:     true,
				MinCapacity: 10,
				MaxCapacity: 20,
				OutCooldown: 100,
				InCooldown:  100,
				TargetValue: 80.0,
			},
		},

		ChunkTables: periodicTableConfig{
			Prefix: chunkTablePrefix,
			Period: tablePeriod,
			From:   util.NewDayValue(model.TimeFromUnix(0)),
			ProvisionedWriteThroughput: write,
			ProvisionedReadThroughput:  read,
			InactiveWriteThroughput:    inactiveWrite,
			InactiveReadThroughput:     inactiveRead,
			WriteScale: autoScalingConfig{
				Enabled:     true,
				MinCapacity: 10,
				MaxCapacity: 20,
				OutCooldown: 100,
				InCooldown:  100,
				TargetValue: 80.0,
			},
		},

		CreationGracePeriod: gracePeriod,
		MaxChunkAge:         maxChunkAge,
	}

	// Check tables are created with autoscale
	{
		tableManager, err := NewTableManager(cfg, client)
		if err != nil {
			t.Fatal(err)
		}

		test(
			tableManager,
			"Create tables",
			time.Unix(0, 0).Add(maxChunkAge).Add(gracePeriod),
			[]TableDesc{
				{
					Name:             "",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
				},
				{
					Name:             tablePrefix + "0",
					ProvisionedRead:  read,
					ProvisionedWrite: write,
					WriteScale: autoScalingConfig{
						Enabled:     true,
						MinCapacity: 10,
						MaxCapacity: 20,
						OutCooldown: 100,
						InCooldown:  100,
						TargetValue: 80.0,
					},
				},
				{
					Name:             chunkTablePrefix + "0",
					ProvisionedRead:  read,
					ProvisionedWrite: write,
					WriteScale: autoScalingConfig{
						Enabled:     true,
						MinCapacity: 10,
						MaxCapacity: 20,
						OutCooldown: 100,
						InCooldown:  100,
						TargetValue: 80.0,
					},
				},
			},
		)
	}

	// Check tables are updated with new settings
	{
		cfg.IndexTables.WriteScale.OutCooldown = 200
		cfg.ChunkTables.WriteScale.TargetValue = 90.0

		tableManager, err := NewTableManager(cfg, client)
		if err != nil {
			t.Fatal(err)
		}

		test(
			tableManager,
			"Update tables with new settings",
			time.Unix(0, 0).Add(maxChunkAge).Add(gracePeriod),
			[]TableDesc{
				{
					Name:             "",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
				},
				{
					Name:             tablePrefix + "0",
					ProvisionedRead:  read,
					ProvisionedWrite: write,
					WriteScale: autoScalingConfig{
						Enabled:     true,
						MinCapacity: 10,
						MaxCapacity: 20,
						OutCooldown: 200,
						InCooldown:  100,
						TargetValue: 80.0,
					},
				},
				{
					Name:             chunkTablePrefix + "0",
					ProvisionedRead:  read,
					ProvisionedWrite: write,
					WriteScale: autoScalingConfig{
						Enabled:     true,
						MinCapacity: 10,
						MaxCapacity: 20,
						OutCooldown: 100,
						InCooldown:  100,
						TargetValue: 90.0,
					},
				},
			},
		)
	}

	// Check tables are degristered when autoscaling is disabled for inactive tables
	{
		cfg.IndexTables.WriteScale.OutCooldown = 200
		cfg.ChunkTables.WriteScale.TargetValue = 90.0

		tableManager, err := NewTableManager(cfg, client)
		if err != nil {
			t.Fatal(err)
		}

		test(
			tableManager,
			"Update tables with new settings",
			time.Unix(0, 0).Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
			[]TableDesc{
				{
					Name:             "",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
				},
				{
					Name:             tablePrefix + "0",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
					WriteScale: autoScalingConfig{
						Enabled: false,
					},
				},
				{
					Name:             chunkTablePrefix + "0",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
					WriteScale: autoScalingConfig{
						Enabled: false,
					},
				},
				{
					Name:             tablePrefix + "1",
					ProvisionedRead:  read,
					ProvisionedWrite: write,
					WriteScale: autoScalingConfig{
						Enabled:     true,
						MinCapacity: 10,
						MaxCapacity: 20,
						OutCooldown: 200,
						InCooldown:  100,
						TargetValue: 80.0,
					},
				},
				{
					Name:             chunkTablePrefix + "1",
					ProvisionedRead:  read,
					ProvisionedWrite: write,
					WriteScale: autoScalingConfig{
						Enabled:     true,
						MinCapacity: 10,
						MaxCapacity: 20,
						OutCooldown: 100,
						InCooldown:  100,
						TargetValue: 90.0,
					},
				},
			},
		)
	}

	// Check tables are degristered when autoscaling is disabled entirely
	{
		cfg.IndexTables.WriteScale.Enabled = false
		cfg.ChunkTables.WriteScale.Enabled = false

		tableManager, err := NewTableManager(cfg, client)
		if err != nil {
			t.Fatal(err)
		}

		test(
			tableManager,
			"Update tables with new settings",
			time.Unix(0, 0).Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
			[]TableDesc{
				{
					Name:             "",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
				},
				{
					Name:             tablePrefix + "0",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
					WriteScale: autoScalingConfig{
						Enabled: false,
					},
				},
				{
					Name:             chunkTablePrefix + "0",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
					WriteScale: autoScalingConfig{
						Enabled: false,
					},
				},
				{
					Name:             tablePrefix + "1",
					ProvisionedRead:  read,
					ProvisionedWrite: write,
					WriteScale: autoScalingConfig{
						Enabled: false,
					},
				},
				{
					Name:             chunkTablePrefix + "1",
					ProvisionedRead:  read,
					ProvisionedWrite: write,
					WriteScale: autoScalingConfig{
						Enabled: false,
					},
				},
			},
		)
	}
}

func TestTableManagerInactiveAutoScaling(t *testing.T) {
	dynamoDB := newMockDynamoDB(0, 0)
	applicationAutoScaling := newMockApplicationAutoScaling()
	client := dynamoTableClient{
		DynamoDB:               dynamoDB,
		ApplicationAutoScaling: applicationAutoScaling,
	}

	test := func(tableManager *TableManager, name string, tm time.Time, expected []TableDesc) {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			mtime.NowForce(tm)
			if err := tableManager.syncTables(ctx); err != nil {
				t.Fatal(err)
			}
			expectTables(ctx, t, client, expected)
		})
	}

	cfg := SchemaConfig{
		UsePeriodicTables: true,
		IndexTables: periodicTableConfig{
			Prefix: tablePrefix,
			Period: tablePeriod,
			From:   util.NewDayValue(model.TimeFromUnix(0)),
			ProvisionedWriteThroughput: write,
			ProvisionedReadThroughput:  read,
			InactiveWriteThroughput:    inactiveWrite,
			InactiveReadThroughput:     inactiveRead,
			InactiveWriteScale: autoScalingConfig{
				Enabled:     true,
				MinCapacity: 10,
				MaxCapacity: 20,
				OutCooldown: 100,
				InCooldown:  100,
				TargetValue: 80.0,
			},
			InactiveWriteScaleLastN: 2,
		},

		ChunkTables: periodicTableConfig{
			Prefix: chunkTablePrefix,
			Period: tablePeriod,
			From:   util.NewDayValue(model.TimeFromUnix(0)),
			ProvisionedWriteThroughput: write,
			ProvisionedReadThroughput:  read,
			InactiveWriteThroughput:    inactiveWrite,
			InactiveReadThroughput:     inactiveRead,
			InactiveWriteScale: autoScalingConfig{
				Enabled:     true,
				MinCapacity: 10,
				MaxCapacity: 20,
				OutCooldown: 100,
				InCooldown:  100,
				TargetValue: 80.0,
			},
			InactiveWriteScaleLastN: 2,
		},

		CreationGracePeriod: gracePeriod,
		MaxChunkAge:         maxChunkAge,
	}

	// Check legacy and latest tables do not autoscale with inactive autoscale enabled.
	{
		tableManager, err := NewTableManager(cfg, client)
		if err != nil {
			t.Fatal(err)
		}

		test(
			tableManager,
			"Legacy and latest tables",
			time.Unix(0, 0).Add(maxChunkAge).Add(gracePeriod),
			[]TableDesc{
				{
					Name:             "",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
				},
				{
					Name:             tablePrefix + "0",
					ProvisionedRead:  read,
					ProvisionedWrite: write,
				},
				{
					Name:             chunkTablePrefix + "0",
					ProvisionedRead:  read,
					ProvisionedWrite: write,
				},
			},
		)
	}

	// Check inactive tables are autoscaled even if there are less than the limit.
	{
		tableManager, err := NewTableManager(cfg, client)
		if err != nil {
			t.Fatal(err)
		}

		test(
			tableManager,
			"1 week of inactive tables with latest",
			time.Unix(0, 0).Add(tablePeriod).Add(maxChunkAge).Add(gracePeriod),
			[]TableDesc{
				{
					Name:             "",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
				},
				{
					Name:             tablePrefix + "0",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
					WriteScale: autoScalingConfig{
						Enabled:     true,
						MinCapacity: 10,
						MaxCapacity: 20,
						OutCooldown: 100,
						InCooldown:  100,
						TargetValue: 80.0,
					},
				},
				{
					Name:             chunkTablePrefix + "0",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
					WriteScale: autoScalingConfig{
						Enabled:     true,
						MinCapacity: 10,
						MaxCapacity: 20,
						OutCooldown: 100,
						InCooldown:  100,
						TargetValue: 80.0,
					},
				},
				{
					Name:             tablePrefix + "1",
					ProvisionedRead:  read,
					ProvisionedWrite: write,
				},
				{
					Name:             chunkTablePrefix + "1",
					ProvisionedRead:  read,
					ProvisionedWrite: write,
				},
			},
		)
	}

	// Check inactive tables past the limit do not autoscale but the latest N do.
	{
		tableManager, err := NewTableManager(cfg, client)
		if err != nil {
			t.Fatal(err)
		}

		test(
			tableManager,
			"3 weeks of inactive tables with latest",
			time.Unix(0, 0).Add(tablePeriod*3).Add(maxChunkAge).Add(gracePeriod),
			[]TableDesc{
				{
					Name:             "",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
				},
				{
					Name:             tablePrefix + "0",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
				},
				{
					Name:             chunkTablePrefix + "0",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
				},
				{
					Name:             tablePrefix + "1",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
					WriteScale: autoScalingConfig{
						Enabled:     true,
						MinCapacity: 10,
						MaxCapacity: 20,
						OutCooldown: 100,
						InCooldown:  100,
						TargetValue: 80.0,
					},
				},
				{
					Name:             chunkTablePrefix + "1",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
					WriteScale: autoScalingConfig{
						Enabled:     true,
						MinCapacity: 10,
						MaxCapacity: 20,
						OutCooldown: 100,
						InCooldown:  100,
						TargetValue: 80.0,
					},
				},
				{
					Name:             tablePrefix + "2",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
					WriteScale: autoScalingConfig{
						Enabled:     true,
						MinCapacity: 10,
						MaxCapacity: 20,
						OutCooldown: 100,
						InCooldown:  100,
						TargetValue: 80.0,
					},
				},
				{
					Name:             chunkTablePrefix + "2",
					ProvisionedRead:  inactiveRead,
					ProvisionedWrite: inactiveWrite,
					WriteScale: autoScalingConfig{
						Enabled:     true,
						MinCapacity: 10,
						MaxCapacity: 20,
						OutCooldown: 100,
						InCooldown:  100,
						TargetValue: 80.0,
					},
				},
				{
					Name:             tablePrefix + "3",
					ProvisionedRead:  read,
					ProvisionedWrite: write,
				},
				{
					Name:             chunkTablePrefix + "3",
					ProvisionedRead:  read,
					ProvisionedWrite: write,
				},
			},
		)
	}
}

func expectTables(ctx context.Context, t *testing.T, dynamo TableClient, expected []TableDesc) {
	tables, err := dynamo.ListTables(ctx)
	if err != nil {
		t.Fatal(err)
	}

	if len(expected) != len(tables) {
		t.Fatalf("Unexpected number of tables: %v != %v", expected, tables)
	}

	sort.Strings(tables)
	sort.Sort(byName(expected))

	for i, expect := range expected {
		if tables[i] != expect.Name {
			t.Fatalf("Expected '%s', found '%s'", expect.Name, tables[i])
		}

		desc, _, err := dynamo.DescribeTable(ctx, expect.Name)
		if err != nil {
			t.Fatal(err)
		}

		if !desc.Equals(expect) {
			t.Fatalf("Expected '%v', found '%v' for table '%s'", expect, desc, desc.Name)
		}
	}
}

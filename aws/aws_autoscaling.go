package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/applicationautoscaling"
	"github.com/aws/aws-sdk-go/service/applicationautoscaling/applicationautoscalingiface"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/weaveworks/common/instrument"
)

const (
	autoScalingPolicyNamePrefix = "DynamoScalingPolicy_cortex_"
)

var applicationAutoScalingRequestDuration = instrument.NewHistogramCollector(prometheus.NewHistogramVec(prometheus.HistogramOpts{
	Namespace: "cortex",
	Name:      "application_autoscaling_request_duration_seconds",
	Help:      "Time spent doing ApplicationAutoScaling requests.",

	// AWS latency seems to range from a few ms to a few sec. So use 8 buckets
	// from 128us to 2s. TODO: Confirm that this is the case for ApplicationAutoScaling.
	Buckets: prometheus.ExponentialBuckets(0.000128, 4, 8),
}, []string{"operation", "status_code"}))

func init() {
	applicationAutoScalingRequestDuration.Register()
}

type awsAutoscale struct {
	call                   callManager
	ApplicationAutoScaling applicationautoscalingiface.ApplicationAutoScalingAPI
}

func newAWSAutoscale(cfg DynamoDBConfig, callManager callManager) (*awsAutoscale, error) {
	session, err := awsSessionFromURL(cfg.ApplicationAutoScaling.URL)
	if err != nil {
		return nil, err
	}
	return &awsAutoscale{
		call:                   callManager,
		ApplicationAutoScaling: applicationautoscaling.New(session),
	}, nil
}

func (a *awsAutoscale) PostCreateTable(ctx context.Context, desc chunk.TableDesc) error {
	if desc.WriteScale.Enabled {
		return a.enableAutoScaling(ctx, desc)
	}
	return nil
}

func (a *awsAutoscale) DescribeTable(ctx context.Context, desc *chunk.TableDesc) error {
	err := a.call.backoffAndRetry(ctx, func(ctx context.Context) error {
		return instrument.CollectedRequest(ctx, "ApplicationAutoScaling.DescribeScalableTargetsWithContext", applicationAutoScalingRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			out, err := a.ApplicationAutoScaling.DescribeScalableTargetsWithContext(ctx, &applicationautoscaling.DescribeScalableTargetsInput{
				ResourceIds:       []*string{aws.String("table/" + desc.Name)},
				ScalableDimension: aws.String("dynamodb:table:WriteCapacityUnits"),
				ServiceNamespace:  aws.String("dynamodb"),
			})
			if err != nil {
				return err
			}
			switch l := len(out.ScalableTargets); l {
			case 0:
				return err
			case 1:
				desc.WriteScale.Enabled = true
				if target := out.ScalableTargets[0]; target != nil {
					if target.RoleARN != nil {
						desc.WriteScale.RoleARN = *target.RoleARN
					}
					if target.MinCapacity != nil {
						desc.WriteScale.MinCapacity = *target.MinCapacity
					}
					if target.MaxCapacity != nil {
						desc.WriteScale.MaxCapacity = *target.MaxCapacity
					}
				}
				return err
			default:
				return fmt.Errorf("more than one scalable target found for DynamoDB table")
			}
		})
	})
	if err != nil {
		return err
	}

	err = a.call.backoffAndRetry(ctx, func(ctx context.Context) error {
		return instrument.CollectedRequest(ctx, "ApplicationAutoScaling.DescribeScalingPoliciesWithContext", applicationAutoScalingRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			out, err := a.ApplicationAutoScaling.DescribeScalingPoliciesWithContext(ctx, &applicationautoscaling.DescribeScalingPoliciesInput{
				PolicyNames:       []*string{aws.String(autoScalingPolicyNamePrefix + desc.Name)},
				ResourceId:        aws.String("table/" + desc.Name),
				ScalableDimension: aws.String("dynamodb:table:WriteCapacityUnits"),
				ServiceNamespace:  aws.String("dynamodb"),
			})
			if err != nil {
				return err
			}
			switch l := len(out.ScalingPolicies); l {
			case 0:
				return err
			case 1:
				config := out.ScalingPolicies[0].TargetTrackingScalingPolicyConfiguration
				if config != nil {
					if config.ScaleInCooldown != nil {
						desc.WriteScale.InCooldown = *config.ScaleInCooldown
					}
					if config.ScaleOutCooldown != nil {
						desc.WriteScale.OutCooldown = *config.ScaleOutCooldown
					}
					if config.TargetValue != nil {
						desc.WriteScale.TargetValue = *config.TargetValue
					}
				}
				return err
			default:
				return fmt.Errorf("more than one scaling policy found for DynamoDB table")
			}
		})
	})
	return err
}

func (a *awsAutoscale) UpdateTable(ctx context.Context, current chunk.TableDesc, expected *chunk.TableDesc) error {
	var err error
	if !current.WriteScale.Enabled {
		if expected.WriteScale.Enabled {
			level.Info(util.Logger).Log("msg", "enabling autoscaling on table", "table")
			err = a.enableAutoScaling(ctx, *expected)
		}
	} else {
		if !expected.WriteScale.Enabled {
			level.Info(util.Logger).Log("msg", "disabling autoscaling on table", "table")
			err = a.disableAutoScaling(ctx, *expected)
		} else if current.WriteScale != expected.WriteScale {
			level.Info(util.Logger).Log("msg", "enabling autoscaling on table", "table")
			err = a.enableAutoScaling(ctx, *expected)
		}
	}
	return err
}

func (a *awsAutoscale) enableAutoScaling(ctx context.Context, desc chunk.TableDesc) error {
	// Registers or updates a scalable target
	if err := a.call.backoffAndRetry(ctx, func(ctx context.Context) error {
		return instrument.CollectedRequest(ctx, "ApplicationAutoScaling.RegisterScalableTarget", applicationAutoScalingRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			input := &applicationautoscaling.RegisterScalableTargetInput{
				MinCapacity:       aws.Int64(desc.WriteScale.MinCapacity),
				MaxCapacity:       aws.Int64(desc.WriteScale.MaxCapacity),
				ResourceId:        aws.String("table/" + desc.Name),
				RoleARN:           aws.String(desc.WriteScale.RoleARN),
				ScalableDimension: aws.String("dynamodb:table:WriteCapacityUnits"),
				ServiceNamespace:  aws.String("dynamodb"),
			}
			_, err := a.ApplicationAutoScaling.RegisterScalableTarget(input)
			if err != nil {
				return err
			}
			return nil
		})
	}); err != nil {
		return err
	}

	// Puts or updates a scaling policy
	return a.call.backoffAndRetry(ctx, func(ctx context.Context) error {
		return instrument.CollectedRequest(ctx, "ApplicationAutoScaling.PutScalingPolicy", applicationAutoScalingRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			input := &applicationautoscaling.PutScalingPolicyInput{
				PolicyName:        aws.String(autoScalingPolicyNamePrefix + desc.Name),
				PolicyType:        aws.String("TargetTrackingScaling"),
				ResourceId:        aws.String("table/" + desc.Name),
				ScalableDimension: aws.String("dynamodb:table:WriteCapacityUnits"),
				ServiceNamespace:  aws.String("dynamodb"),
				TargetTrackingScalingPolicyConfiguration: &applicationautoscaling.TargetTrackingScalingPolicyConfiguration{
					PredefinedMetricSpecification: &applicationautoscaling.PredefinedMetricSpecification{
						PredefinedMetricType: aws.String("DynamoDBWriteCapacityUtilization"),
					},
					ScaleInCooldown:  aws.Int64(desc.WriteScale.InCooldown),
					ScaleOutCooldown: aws.Int64(desc.WriteScale.OutCooldown),
					TargetValue:      aws.Float64(desc.WriteScale.TargetValue),
				},
			}
			_, err := a.ApplicationAutoScaling.PutScalingPolicy(input)
			return err
		})
	})
}

func (a *awsAutoscale) disableAutoScaling(ctx context.Context, desc chunk.TableDesc) error {
	// Deregister scalable target
	if err := a.call.backoffAndRetry(ctx, func(ctx context.Context) error {
		return instrument.CollectedRequest(ctx, "ApplicationAutoScaling.DeregisterScalableTarget", applicationAutoScalingRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			input := &applicationautoscaling.DeregisterScalableTargetInput{
				ResourceId:        aws.String("table/" + desc.Name),
				ScalableDimension: aws.String("dynamodb:table:WriteCapacityUnits"),
				ServiceNamespace:  aws.String("dynamodb"),
			}
			_, err := a.ApplicationAutoScaling.DeregisterScalableTarget(input)
			return err
		})
	}); err != nil {
		return err
	}

	// Delete scaling policy
	return a.call.backoffAndRetry(ctx, func(ctx context.Context) error {
		return instrument.CollectedRequest(ctx, "ApplicationAutoScaling.DeleteScalingPolicy", applicationAutoScalingRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
			input := &applicationautoscaling.DeleteScalingPolicyInput{
				PolicyName:        aws.String(autoScalingPolicyNamePrefix + desc.Name),
				ResourceId:        aws.String("table/" + desc.Name),
				ScalableDimension: aws.String("dynamodb:table:WriteCapacityUnits"),
				ServiceNamespace:  aws.String("dynamodb"),
			}
			_, err := a.ApplicationAutoScaling.DeleteScalingPolicy(input)
			return err
		})
	})
}

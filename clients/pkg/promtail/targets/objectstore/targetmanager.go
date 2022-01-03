package objectstore

import (
	//"github.com/cortexproject/cortex/pkg/chunk/aws"
	"github.com/go-kit/log"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/clients/pkg/logentry/stages"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
	"github.com/grafana/loki/pkg/storage/chunk/aws"
	"github.com/grafana/loki/pkg/storage/chunk/hedging"
)

// TargetManager manages a series of cloudflare targets.
type TargetManager struct {
	logger  log.Logger
	targets map[string]*Target
}

// NewTargetManager creates a new cloudflare target managers.
func NewTargetManager(
	metrics *Metrics,
	logger log.Logger,
	positions positions.Positions,
	pushClient api.EntryHandler,
	scrapeConfigs []scrapeconfig.Config,
) (*TargetManager, error) {
	// tm := &TargetManager{
	// 	logger:  logger,
	// 	targets: make(map[string]*Target),
	// }
	// for _, cfg := range scrapeConfigs {
	// 	if cfg.CloudflareConfig == nil {
	// 		continue
	// 	}
	// 	pipeline, err := stages.NewPipeline(log.With(logger, "component", "cloudflare_pipeline"), cfg.PipelineStages, &cfg.JobName, metrics.reg)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	t, err := NewTarget(metrics, log.With(logger, "target", "cloudflare"), pipeline.Wrap(pushClient), positions, cfg.CloudflareConfig)
	// 	if err != nil {
	// 		return nil, err
	// 	}
	// 	tm.targets[cfg.JobName] = t
	// }

	// return tm, nil
	tm := &TargetManager{
		logger:  logger,
		targets: make(map[string]*Target),
	}

	for _, cfg := range scrapeConfigs {
		switch {
		case cfg.S3Config != nil:
			pipeline, err := stages.NewPipeline(log.With(logger, "component", "file_pipeline"), cfg.PipelineStages, &cfg.JobName, metrics.reg)
			if err != nil {
				return nil, err
			}

			storageClient := aws.S3Config{
				BucketNames:      cfg.S3Config.BucketName,
				Endpoint:         cfg.S3Config.Endpoint,
				Region:           cfg.S3Config.Region,
				AccessKeyID:      cfg.S3Config.AccessKeyID,
				SecretAccessKey:  cfg.S3Config.SecretAccessKey,
				Insecure:         cfg.S3Config.Insecure,
				S3ForcePathStyle: cfg.S3Config.S3ForcePathStyle,
				HTTPConfig:       cfg.S3Config.HTTPConfig,
			}

			objectClient, err := aws.NewS3ObjectClient(storageClient, hedging.Config{})
			if err != nil {
				return nil, err
			}

			sqsClient := SQSConfig{
				storageClient,
				cfg.S3Config.SQSQueue,
			}

			s3Client, err := newS3Client(sqsClient)
			if err != nil {
				return nil, err
			}

			labels := cfg.S3Config.Labels.Merge(model.LabelSet{
				model.LabelName("s3_bucket"): model.LabelValue(cfg.S3Config.BucketName),
			})

			t, err := NewTarget(metrics, log.With(logger, "target", "objectstore"), pipeline.Wrap(pushClient), positions, objectClient, s3Client, labels, cfg.S3Config.Timeout, cfg.S3Config.ResetCursor, "s3")
			if err != nil {
				return nil, err
			}

			tm.targets[cfg.JobName] = t
		}

	}

	return tm, nil
}

// Ready returns true if at least one cloudflare target is active.
func (tm *TargetManager) Ready() bool {
	for _, t := range tm.targets {
		if t.Ready() {
			return true
		}
	}
	return false
}

func (tm *TargetManager) Stop() {
	for _, t := range tm.targets {
		t.Stop()
	}
}

func (tm *TargetManager) ActiveTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.targets))
	for k, v := range tm.targets {
		if v.Ready() {
			result[k] = []target.Target{v}
		}
	}
	return result
}

func (tm *TargetManager) AllTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.targets))
	for k, v := range tm.targets {
		result[k] = []target.Target{v}
	}
	return result
}

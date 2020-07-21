package objectstore

import (
	"time"

	"github.com/cortexproject/cortex/pkg/chunk/aws"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/positions"
	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/pkg/promtail/targets/target"
)

// S3TargetManager manages a set of S3 targets.
type S3TargetManager struct {
	log     log.Logger
	targets map[string]*ObjectTarget
}

// NewS3TargetManager creates a new S3TargetManager.
func NewS3TargetManager(
	logger log.Logger,
	positions positions.Positions,
	client api.EntryHandler,
	scrapeConfigs []scrapeconfig.Config,
) (*S3TargetManager, error) {
	tm := &S3TargetManager{
		log:     logger,
		targets: make(map[string]*ObjectTarget),
	}

	for _, cfg := range scrapeConfigs {
		registerer := prometheus.DefaultRegisterer
		pipeline, err := stages.NewPipeline(log.With(logger, "component", "file_pipeline"), cfg.PipelineStages, &cfg.JobName, registerer)
		if err != nil {
			return nil, err
		}

		awsHTTPConfig := aws.HTTPConfig{
			IdleConnTimeout:       cfg.S3Config.HTTPConfig.IdleConnTimeout,
			ResponseHeaderTimeout: cfg.S3Config.HTTPConfig.ResponseHeaderTimeout,
			InsecureSkipVerify:    cfg.S3Config.HTTPConfig.InsecureSkipVerify,
		}

		if cfg.S3Config.HTTPConfig.IdleConnTimeout == 0 {
			awsHTTPConfig.IdleConnTimeout = 90 * time.Second
		}

		storageClient := aws.S3Config{
			BucketNames:      cfg.S3Config.BucketName,
			Endpoint:         cfg.S3Config.Endpoint,
			Region:           cfg.S3Config.Region,
			AccessKeyID:      cfg.S3Config.AccessKeyID,
			SecretAccessKey:  cfg.S3Config.SecretAccessKey,
			Insecure:         cfg.S3Config.Insecure,
			S3ForcePathStyle: cfg.S3Config.S3ForcePathStyle,
			HTTPConfig:       awsHTTPConfig,
		}

		objectClient, err := aws.NewS3ObjectClient(storageClient, ",")
		if err != nil {
			return nil, err
		}

		t, err := NewObjectTarget(logger, pipeline.Wrap(client), positions, cfg.JobName, objectClient, &cfg)
		if err != nil {
			return nil, err
		}

		tm.targets[cfg.JobName] = t
	}

	return tm, nil
}

// Ready returns true if at least one S3Target is ready.
func (tm *S3TargetManager) Ready() bool {
	for _, t := range tm.targets {
		if t.Ready() {
			return true
		}
	}
	return false
}

// Stop stops the S3TargetManager and all of its S3Targets.
func (tm *S3TargetManager) Stop() {
	for key, target := range tm.targets {
		level.Info(tm.log).Log("msg", "Removing target", "key", key)
		target.Stop()
		delete(tm.targets, key)
	}
}

// ActiveTargets returns the list of S3Targets.
func (tm *S3TargetManager) ActiveTargets() map[string][]target.Target {
	return tm.AllTargets()
}

// AllTargets returns the list of all S3 targets which
// is currently being read.
func (tm *S3TargetManager) AllTargets() map[string][]target.Target {
	result := make(map[string][]target.Target, len(tm.targets))
	for k, v := range tm.targets {
		result[k] = []target.Target{v}
	}
	return result
}

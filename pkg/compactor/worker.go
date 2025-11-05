package compactor

import (
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/compactor/jobqueue"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

func NewWorkerManager(
	cfg Config,
	grpcClient jobqueue.CompactorClient,
	schemaConfig config.SchemaConfig,
	chunkClients map[config.DayTime]client.Client,
	r prometheus.Registerer,
) (services.Service, error) {
	wm := jobqueue.NewWorkerManager(cfg.WorkerConfig, grpcClient, r)

	if cfg.RetentionEnabled {
		deletionJobRunner := initDeletionJobRunner(cfg.JobsConfig.Deletion.ChunkProcessingConcurrency, schemaConfig, chunkClients, r)
		err := wm.RegisterJobRunner(grpc.JOB_TYPE_DELETION, deletionJobRunner)
		if err != nil {
			return nil, err
		}
	}

	return services.NewBasicService(nil, wm.Start, nil), nil
}

func initDeletionJobRunner(
	chunkProcessingConcurrency int,
	schemaConfig config.SchemaConfig,
	chunkClients map[config.DayTime]client.Client,
	r prometheus.Registerer,
) jobqueue.JobRunner {
	return deletion.NewJobRunner(chunkProcessingConcurrency, func(table string) (client.Client, error) {
		schemaCfg, ok := SchemaPeriodForTable(schemaConfig, table)
		if !ok {
			return nil, errSchemaForTableNotFound
		}

		return chunkClients[schemaCfg.From], nil
	}, r)
}

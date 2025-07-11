package compactor

import (
	"context"

	"github.com/grafana/dskit/services"

	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/compactor/jobqueue"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

func NewWorkerManager(cfg Config, grpcClient jobqueue.CompactorClient, schemaConfig config.SchemaConfig, objectStoreClients map[config.DayTime]client.ObjectClient) (services.Service, error) {
	wm := jobqueue.NewWorkerManager(cfg.WorkerConfig, grpcClient)

	if cfg.RetentionEnabled {
		deletionJobRunner := initDeletionJobRunner(cfg.JobsConfig.Deletion.ChunkProcessingConcurrency, schemaConfig, objectStoreClients)
		err := wm.RegisterJobRunner(grpc.JOB_TYPE_DELETION, deletionJobRunner)
		if err != nil {
			return nil, err
		}
	}

	return services.NewBasicService(nil, wm.Start, nil), nil
}

func initDeletionJobRunner(chunkProcessingConcurrency int, schemaConfig config.SchemaConfig, objectStoreClients map[config.DayTime]client.ObjectClient) jobqueue.JobRunner {
	chunkClients := make(map[config.DayTime]client.Client, len(objectStoreClients))
	for from, objectClient := range objectStoreClients {
		var (
			raw     client.ObjectClient
			encoder client.KeyEncoder
		)

		if casted, ok := objectClient.(client.PrefixedObjectClient); ok {
			raw = casted.GetDownstream()
		} else {
			raw = objectClient
		}
		if _, ok := raw.(*local.FSObjectClient); ok {
			encoder = client.FSEncoder
		}
		chunkClients[from] = client.NewClient(objectClient, encoder, schemaConfig)
	}

	return deletion.NewJobRunner(chunkProcessingConcurrency, func(ctx context.Context, table string) (client.Client, error) {
		schemaCfg, ok := SchemaPeriodForTable(schemaConfig, table)
		if !ok {
			return nil, errSchemaForTableNotFound
		}

		return chunkClients[schemaCfg.From], nil
	})
}

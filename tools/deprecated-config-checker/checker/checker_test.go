package checker

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	deprecatesFilePath = "../deprecated-config.yaml"
	deletesFilePath    = "../deleted-config.yaml"
	configPath         = "../test-fixtures/config.yaml"
	runtimeConfigPath  = "../test-fixtures/runtime-config.yaml"
)

var (
	expectedConfigDeletes = []string{
		"ingester.max_transfer_retries",
		"querier.engine.timeout",
		"query_range.split_queries_by_interval",
		"query_range.forward_headers_list",
		"frontend_worker.parallelism",
		"frontend_worker.match_max_concurrent",
		"common.storage.s3.sse_encryption",
		"ruler.storage.s3.sse_encryption",
		"storage_config.boltdb_shipper.use_boltdb_shipper_as_backup",
		"storage_config.aws.sse_encryption",
		"storage_config.s3.sse_encryption",
		"chunk_store_config.max_look_back_period",
		"storage_config.boltdb_shipper.shared_store",
		"storage_config.boltdb_shipper.shared_store_key_prefix",
		"storage_config.tsdb_shipper.shared_store",
		"storage_config.tsdb_shipper.shared_store_key_prefix",
		"compactor.deletion_mode",
		"compactor.shared_store",
		"compactor.shared_store_key_prefix",
		"limits_config.enforce_metric_name",
		"limits_config.ruler_evaluation_delay_duration",
	}

	expectedConfigDeprecates = []string{
		"legacy-read-mode",
		"ruler.remote_write.client",
		"index_gateway.ring.replication_factor",
		"storage_config.bigtable",
		"storage_config.cassandra",
		"storage_config.boltdb",
		"storage_config.grpc_store",
		"storage_config.aws.dynamodb",
		"chunk_store_config.write_dedupe_cache_config",
		"limits_config.unordered_writes",
		"limits_config.ruler_remote_write_url",
		"limits_config.ruler_remote_write_timeout",
		"limits_config.ruler_remote_write_headers",
		"limits_config.ruler_remote_write_relabel_configs",
		"limits_config.ruler_remote_write_queue_capacity",
		"limits_config.ruler_remote_write_queue_min_shards",
		"limits_config.ruler_remote_write_queue_max_shards",
		"limits_config.ruler_remote_write_queue_max_samples_per_send",
		"limits_config.ruler_remote_write_queue_batch_send_deadline",
		"limits_config.ruler_remote_write_queue_min_backoff",
		"limits_config.ruler_remote_write_queue_max_backoff",
		"limits_config.ruler_remote_write_queue_retry_on_ratelimit",
		"limits_config.ruler_remote_write_sigv4_config",
		"limits_config.per_tenant_override_config",
		"limits_config.per_tenant_override_period",
		"limits_config.allow_deletes",
		"schema_config.configs.[1].store",
		"schema_config.configs.[1].object_store",
		"schema_config.configs.[2].store",
		"schema_config.configs.[2].object_store",
		"schema_config.configs.[3].store",
		"schema_config.configs.[3].object_store",
		"schema_config.configs.[4].store",
		"schema_config.configs.[4].object_store",
		"schema_config.configs.[5].store",
		"schema_config.configs.[5].object_store",
		"schema_config.configs.[6].store",
		"schema_config.configs.[6].object_store",
		"schema_config.configs.[7].store",
		"schema_config.configs.[7].object_store",
		"schema_config.configs.[8].store",
		"schema_config.configs.[8].object_store",
	}

	expectedRuntimeConfigDeletes = []string{
		"overrides.foo.ruler_evaluation_delay_duration",
		"overrides.foo.enforce_metric_name",
		"overrides.bar.ruler_evaluation_delay_duration",
		"overrides.bar.enforce_metric_name",
	}

	expectedRuntimeConfigDeprecates = []string{
		"overrides.foo.unordered_writes",
		"overrides.foo.ruler_remote_write_url",
		"overrides.foo.ruler_remote_write_timeout",
		"overrides.foo.ruler_remote_write_headers",
		"overrides.foo.ruler_remote_write_relabel_configs",
		"overrides.foo.ruler_remote_write_queue_capacity",
		"overrides.foo.ruler_remote_write_queue_min_shards",
		"overrides.foo.ruler_remote_write_queue_max_shards",
		"overrides.foo.ruler_remote_write_queue_max_samples_per_send",
		"overrides.foo.ruler_remote_write_queue_batch_send_deadline",
		"overrides.foo.ruler_remote_write_queue_min_backoff",
		"overrides.foo.ruler_remote_write_queue_max_backoff",
		"overrides.foo.ruler_remote_write_queue_retry_on_ratelimit",
		"overrides.foo.ruler_remote_write_sigv4_config",
		"overrides.foo.per_tenant_override_config",
		"overrides.foo.per_tenant_override_period",
		"overrides.foo.allow_deletes",
		"overrides.bar.unordered_writes",
		"overrides.bar.ruler_remote_write_url",
		"overrides.bar.ruler_remote_write_timeout",
		"overrides.bar.ruler_remote_write_headers",
		"overrides.bar.ruler_remote_write_relabel_configs",
		"overrides.bar.ruler_remote_write_queue_capacity",
		"overrides.bar.ruler_remote_write_queue_min_shards",
		"overrides.bar.ruler_remote_write_queue_max_shards",
		"overrides.bar.ruler_remote_write_queue_max_samples_per_send",
		"overrides.bar.ruler_remote_write_queue_batch_send_deadline",
		"overrides.bar.ruler_remote_write_queue_min_backoff",
		"overrides.bar.ruler_remote_write_queue_max_backoff",
		"overrides.bar.ruler_remote_write_queue_retry_on_ratelimit",
		"overrides.bar.ruler_remote_write_sigv4_config",
		"overrides.bar.per_tenant_override_config",
		"overrides.bar.per_tenant_override_period",
		"overrides.bar.allow_deletes",
	}
)

func TestConfigDeprecatesAndDeletes(t *testing.T) {
	cfg := Config{
		DeprecatesFile: deprecatesFilePath,
		DeletesFile:    deletesFilePath,
		ConfigFile:     configPath,
	}

	c, err := NewChecker(cfg)
	require.NoError(t, err)

	deprecates := c.CheckConfigDeprecated()
	deprecatesPaths := make([]string, 0, len(deprecates))
	for _, d := range deprecates {
		deprecatesPaths = append(deprecatesPaths, d.ItemPath)
	}
	require.ElementsMatch(t, expectedConfigDeprecates, deprecatesPaths)

	deletes := c.CheckConfigDeleted()
	deletesPaths := make([]string, 0, len(deletes))
	for _, d := range deletes {
		deletesPaths = append(deletesPaths, d.ItemPath)
	}
	require.ElementsMatch(t, expectedConfigDeletes, deletesPaths)
}

func TestRuntimeConfigDeprecatesAndDeletes(t *testing.T) {
	cfg := Config{
		DeprecatesFile:    deprecatesFilePath,
		DeletesFile:       deletesFilePath,
		RuntimeConfigFile: runtimeConfigPath,
	}

	c, err := NewChecker(cfg)
	require.NoError(t, err)

	deprecates := c.CheckRuntimeConfigDeprecated()
	deprecatesPaths := make([]string, 0, len(deprecates))
	for _, d := range deprecates {
		deprecatesPaths = append(deprecatesPaths, d.ItemPath)
	}
	require.ElementsMatch(t, expectedRuntimeConfigDeprecates, deprecatesPaths)

	deletes := c.CheckRuntimeConfigDeleted()
	deletesPaths := make([]string, 0, len(deletes))
	for _, d := range deletes {
		deletesPaths = append(deletesPaths, d.ItemPath)
	}
	require.ElementsMatch(t, expectedRuntimeConfigDeletes, deletesPaths)
}

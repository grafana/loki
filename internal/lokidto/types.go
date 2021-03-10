package lokidto

import (
	"time"
)

type Config struct {
	AuthEnabled      bool              `yaml:"auth_enabled,omitempty"`
	ChunkStoreConfig *ChunkStoreConfig `yaml:"chunk_store_config,omitempty"`
	Compactor        *Compactor        `yaml:"compactor,omitempty"`
	Distributor      *Distributor      `yaml:"distributor,omitempty"`
	Frontend         *Frontend         `yaml:"frontend,omitempty"`
	FrontendWorker   *FrontendWorker   `yaml:"frontend_worker,omitempty"`
	Ingester         *Ingester         `yaml:"ingester,omitempty"`
	IngesterClient   *IngesterClient   `yaml:"ingester_client,omitempty"`
	LimitsConfig     *LimitsConfig     `yaml:"limits_config,omitempty"`
	Memberlist       *Memberlist       `yaml:"memberlist,omitempty"`
	Querier          *Querier          `yaml:"querier,omitempty"`
	QueryRange       *QueryRange       `yaml:"query_range,omitempty"`
	SchemaConfig     *SchemaConfig     `yaml:"schema_config,omitempty"`
	Server           *Server           `yaml:"server,omitempty"`
	StorageConfig    *StorageConfig    `yaml:"storage_config,omitempty"`
	Tracing          *Tracing          `yaml:"tracing,omitempty"`
}

type Memcached struct {
	// Expiration configures how long keys stay in memcached.
	Expiration time.Duration `yaml:"expiration,omitempty"`
	// BatchSize Configures how many keys to fetch in each batch request.
	BatchSize int `yaml:"batch_size,omitempty"`
	// Parallelism sets the maximum active requests to memcached.
	Parallelism int `yaml:"parallelism,omitempty"`
}

type MemcachedClient struct {
	Addresses      string `yaml:"addresses,omitempty"`
	ConsistentHash bool   `yaml:"consistent_hash,omitempty"`
	MaxIdleConns   int    `yaml:"max_idle_conns,omitempty"`
	Timeout        string `yaml:"timeout,omitempty"`
	UpdateInterval string `yaml:"update_interval,omitempty"`
}

type CacheConfig struct {
	Memcached       *Memcached       `yaml:"memcached,omitempty"`
	MemcachedClient *MemcachedClient `yaml:"memcached_client,omitempty"`
	FIFOCache       *FIFOCache       `yaml:"fifocache,omitempty"`
}

type FIFOCache struct {
	// Maximum memory size of the cache in bytes. A unit suffix (KB, MB, GB) may be applied.
	MaxSizeBytes string `yaml:"max_size_bytes,omitempty"`
	// Maximum number of entries in the cache.
	MaxSizeItems int `yaml:"max_size_items,omitempty"`
	// Validity is the expiry duration for the cache.
	Validity time.Duration `yaml:"validity,omitempty"`
}

type ChunkStoreConfig struct {
	ChunkCacheConfig  *CacheConfig `yaml:"chunk_cache_config,omitempty"`
	MaxLookBackPeriod string       `yaml:"max_look_back_period,omitempty"`
}

type Compactor struct {
	CompactionInterval string `yaml:"compaction_interval,omitempty"`
	SharedStore        string `yaml:"shared_store,omitempty"`
	WorkingDirectory   string `yaml:"working_directory,omitempty"`
}

type Kvstore struct {
	Store string `yaml:"store,omitempty"`
}

type Distributor struct {
	Ring Ring `yaml:"ring,omitempty"`
}

type Frontend struct {
	CompressResponses       bool `yaml:"compress_responses,omitempty"`
	MaxOutstandingPerTenant int  `yaml:"max_outstanding_per_tenant,omitempty"`
}

type FrontendWorker struct {
	FrontendAddress  string            `yaml:"frontend_address,omitempty"`
	GrpcClientConfig *GrpcClientConfig `yaml:"grpc_client_config,omitempty"`
	Parallelism      int               `yaml:"parallelism,omitempty"`
}

type Ring struct {
	HeartbeatTimeout string   `yaml:"heartbeat_timeout,omitempty"`
	Kvstore          *Kvstore `yaml:"kvstore,omitempty"`
}

type Lifecycler struct {
	HeartbeatPeriod string   `yaml:"heartbeat_period,omitempty"`
	InterfaceNames  []string `yaml:"interface_names,omitempty"`
	JoinAfter       string   `yaml:"join_after,omitempty"`
	NumTokens       int      `yaml:"num_tokens,omitempty"`
	Ring            *Ring    `yaml:"ring,omitempty"`
}

type Ingester struct {
	ChunkBlockSize     int         `yaml:"chunk_block_size,omitempty"`
	ChunkEncoding      string      `yaml:"chunk_encoding,omitempty"`
	ChunkIdlePeriod    string      `yaml:"chunk_idle_period,omitempty"`
	ChunkRetainPeriod  string      `yaml:"chunk_retain_period,omitempty"`
	ChunkTargetSize    int         `yaml:"chunk_target_size,omitempty"`
	Lifecycler         *Lifecycler `yaml:"lifecycler,omitempty"`
	MaxTransferRetries int         `yaml:"max_transfer_retries,omitempty"`
}

type GrpcClientConfig struct {
	MaxRecvMsgSize int `yaml:"max_recv_msg_size,omitempty"`
	MaxSendMsgSize int `yaml:"max_send_msg_size,omitempty"`
}

type IngesterClient struct {
	GrpcClientConfig *GrpcClientConfig `yaml:"grpc_client_config,omitempty"`
	RemoteTimeout    string            `yaml:"remote_timeout,omitempty"`
}

type LimitsConfig struct {
	EnforceMetricName         bool   `yaml:"enforce_metric_name,omitempty"`
	IngestionBurstSizeMb      int    `yaml:"ingestion_burst_size_mb,omitempty"`
	IngestionRateMb           int    `yaml:"ingestion_rate_mb,omitempty"`
	IngestionRateStrategy     string `yaml:"ingestion_rate_strategy,omitempty"`
	MaxCacheFreshnessPerQuery string `yaml:"max_cache_freshness_per_query,omitempty"`
	MaxGlobalStreamsPerUser   int    `yaml:"max_global_streams_per_user,omitempty"`
	MaxQueryLength            string `yaml:"max_query_length,omitempty"`
	MaxQueryParallelism       int    `yaml:"max_query_parallelism,omitempty"`
	MaxStreamsPerUser         int    `yaml:"max_streams_per_user,omitempty"`
	RejectOldSamples          bool   `yaml:"reject_old_samples,omitempty"`
	RejectOldSamplesMaxAge    string `yaml:"reject_old_samples_max_age,omitempty"`
}

type Memberlist struct {
	AbortIfClusterJoinFails bool     `yaml:"abort_if_cluster_join_fails,omitempty"`
	BindPort                int      `yaml:"bind_port,omitempty"`
	JoinMembers             []string `yaml:"join_members,omitempty"`
	MaxJoinBackoff          string   `yaml:"max_join_backoff,omitempty"`
	MaxJoinRetries          int      `yaml:"max_join_retries,omitempty"`
	MinJoinBackoff          string   `yaml:"min_join_backoff,omitempty"`
}

type Engine struct {
	MaxLookBackPeriod string `yaml:"max_look_back_period,omitempty"`
	Timeout           string `yaml:"timeout,omitempty"`
}

type Querier struct {
	Engine               *Engine `yaml:"engine,omitempty"`
	ExtraQueryDelay      string  `yaml:"extra_query_delay,omitempty"`
	QueryIngestersWithin string  `yaml:"query_ingesters_within,omitempty"`
	QueryTimeout         string  `yaml:"query_timeout,omitempty"`
	TailMaxDuration      string  `yaml:"tail_max_duration,omitempty"`
}

type Cache struct {
	MemcachedClient *MemcachedClient `yaml:"memcached_client,omitempty"`
}

type ResultsCache struct {
	Cache Cache `yaml:"cache,omitempty"`
}

type QueryRange struct {
	AlignQueriesWithStep   bool          `yaml:"align_queries_with_step,omitempty"`
	CacheResults           bool          `yaml:"cache_results,omitempty"`
	MaxRetries             int           `yaml:"max_retries,omitempty"`
	ResultsCache           *ResultsCache `yaml:"results_cache,omitempty"`
	SplitQueriesByInterval string        `yaml:"split_queries_by_interval,omitempty"`
}

type Index struct {
	Period string `yaml:"period,omitempty"`
	Prefix string `yaml:"prefix,omitempty"`
}

type Configs struct {
	From        string `yaml:"from,omitempty"`
	Index       *Index `yaml:"index,omitempty"`
	ObjectStore string `yaml:"object_store,omitempty"`
	Schema      string `yaml:"schema,omitempty"`
	Store       string `yaml:"store,omitempty"`
}

type SchemaConfig struct {
	Configs []Configs `yaml:"configs,omitempty"`
}

type Server struct {
	GracefulShutdownTimeout        string `yaml:"graceful_shutdown_timeout,omitempty"`
	GrpcServerMaxConcurrentStreams int    `yaml:"grpc_server_max_concurrent_streams,omitempty"`
	GrpcServerMaxRecvMsgSize       int    `yaml:"grpc_server_max_recv_msg_size,omitempty"`
	GrpcServerMaxSendMsgSize       int    `yaml:"grpc_server_max_send_msg_size,omitempty"`
	HTTPListenPort                 int    `yaml:"http_listen_port,omitempty"`
	HTTPServerIdleTimeout          string `yaml:"http_server_idle_timeout,omitempty"`
	HTTPServerWriteTimeout         string `yaml:"http_server_write_timeout,omitempty"`
}

type BoltdbShipper struct {
	ActiveIndexDirectory string `yaml:"active_index_directory,omitempty"`
	CacheLocation        string `yaml:"cache_location,omitempty"`
	CacheTTL             string `yaml:"cache_ttl,omitempty"`
	ResyncInterval       string `yaml:"resync_interval,omitempty"`
	SharedStore          string `yaml:"shared_store,omitempty"`
}

type StorageConfig struct {
	BoltdbShipper           *BoltdbShipper `yaml:"boltdb_shipper,omitempty"`
	BoltDB                  *BoltDB        `yaml:"boltdb,omitempty"`
	IndexQueriesCacheConfig *CacheConfig   `yaml:"index_queries_cache_config,omitempty"`
	Filesystem              *Filesystem    `yaml:"filesystem,omitempty"`
}

type Filesystem struct {
	Directory string `yaml:"directory,omitempty"`
}

type BoltDB struct {
	Directory string `yaml:"directory,omitempty"`
}

type Tracing struct {
	Enabled bool `yaml:"enabled,omitempty"`
}

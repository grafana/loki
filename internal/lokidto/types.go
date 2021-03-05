package lokidto

type Config struct {
	AuthEnabled      bool             `yaml:"auth_enabled"`
	ChunkStoreConfig ChunkStoreConfig `yaml:"chunk_store_config"`
	Compactor        Compactor        `yaml:"compactor"`
	Distributor      Distributor      `yaml:"distributor"`
	Frontend         Frontend         `yaml:"frontend"`
	FrontendWorker   FrontendWorker   `yaml:"frontend_worker"`
	Ingester         Ingester         `yaml:"ingester"`
	IngesterClient   IngesterClient   `yaml:"ingester_client"`
	LimitsConfig     LimitsConfig     `yaml:"limits_config"`
	Memberlist       Memberlist       `yaml:"memberlist"`
	Querier          Querier          `yaml:"querier"`
	QueryRange       QueryRange       `yaml:"query_range"`
	SchemaConfig     SchemaConfig     `yaml:"schema_config"`
	Server           Server           `yaml:"server"`
	StorageConfig    StorageConfig    `yaml:"storage_config"`
	Tracing          Tracing          `yaml:"tracing"`
}

type Memcached struct {
	BatchSize   int `yaml:"batch_size"`
	Parallelism int `yaml:"parallelism"`
}

type MemcachedClient struct {
	Addresses      string `yaml:"addresses"`
	ConsistentHash bool   `yaml:"consistent_hash"`
	MaxIdleConns   int    `yaml:"max_idle_conns"`
	Timeout        string `yaml:"timeout"`
	UpdateInterval string `yaml:"update_interval"`
}

type ChunkCacheConfig struct {
	Memcached       Memcached       `yaml:"memcached"`
	MemcachedClient MemcachedClient `yaml:"memcached_client"`
}

type ChunkStoreConfig struct {
	ChunkCacheConfig  ChunkCacheConfig `yaml:"chunk_cache_config"`
	MaxLookBackPeriod string           `yaml:"max_look_back_period"`
}

type Compactor struct {
	CompactionInterval string `yaml:"compaction_interval"`
	SharedStore        string `yaml:"shared_store"`
	WorkingDirectory   string `yaml:"working_directory"`
}

type Kvstore struct {
	Store string `yaml:"store"`
}

type Distributor struct {
	Ring Ring `yaml:"ring"`
}

type Frontend struct {
	CompressResponses       bool `yaml:"compress_responses"`
	MaxOutstandingPerTenant int  `yaml:"max_outstanding_per_tenant"`
}

type FrontendWorker struct {
	FrontendAddress  string           `yaml:"frontend_address"`
	GrpcClientConfig GrpcClientConfig `yaml:"grpc_client_config"`
	Parallelism      int              `yaml:"parallelism"`
}

type Ring struct {
	HeartbeatTimeout string  `yaml:"heartbeat_timeout"`
	Kvstore          Kvstore `yaml:"kvstore"`
}

type Lifecycler struct {
	HeartbeatPeriod string   `yaml:"heartbeat_period"`
	InterfaceNames  []string `yaml:"interface_names"`
	JoinAfter       string   `yaml:"join_after"`
	NumTokens       int      `yaml:"num_tokens"`
	Ring            Ring     `yaml:"ring"`
}

type Ingester struct {
	ChunkBlockSize     int        `yaml:"chunk_block_size"`
	ChunkEncoding      string     `yaml:"chunk_encoding"`
	ChunkIdlePeriod    string     `yaml:"chunk_idle_period"`
	ChunkRetainPeriod  string     `yaml:"chunk_retain_period"`
	ChunkTargetSize    int        `yaml:"chunk_target_size"`
	Lifecycler         Lifecycler `yaml:"lifecycler"`
	MaxTransferRetries int        `yaml:"max_transfer_retries"`
}

type GrpcClientConfig struct {
	MaxRecvMsgSize int `yaml:"max_recv_msg_size"`
	MaxSendMsgSize int `yaml:"max_send_msg_size"`
}

type IngesterClient struct {
	GrpcClientConfig GrpcClientConfig `yaml:"grpc_client_config"`
	RemoteTimeout    string           `yaml:"remote_timeout"`
}

type LimitsConfig struct {
	EnforceMetricName         bool   `yaml:"enforce_metric_name"`
	IngestionBurstSizeMb      int    `yaml:"ingestion_burst_size_mb"`
	IngestionRateMb           int    `yaml:"ingestion_rate_mb"`
	IngestionRateStrategy     string `yaml:"ingestion_rate_strategy"`
	MaxCacheFreshnessPerQuery string `yaml:"max_cache_freshness_per_query"`
	MaxGlobalStreamsPerUser   int    `yaml:"max_global_streams_per_user"`
	MaxQueryLength            string `yaml:"max_query_length"`
	MaxQueryParallelism       int    `yaml:"max_query_parallelism"`
	MaxStreamsPerUser         int    `yaml:"max_streams_per_user"`
	RejectOldSamples          bool   `yaml:"reject_old_samples"`
	RejectOldSamplesMaxAge    string `yaml:"reject_old_samples_max_age"`
}

type Memberlist struct {
	AbortIfClusterJoinFails bool     `yaml:"abort_if_cluster_join_fails"`
	BindPort                int      `yaml:"bind_port"`
	JoinMembers             []string `yaml:"join_members"`
	MaxJoinBackoff          string   `yaml:"max_join_backoff"`
	MaxJoinRetries          int      `yaml:"max_join_retries"`
	MinJoinBackoff          string   `yaml:"min_join_backoff"`
}

type Engine struct {
	MaxLookBackPeriod string `yaml:"max_look_back_period"`
	Timeout           string `yaml:"timeout"`
}

type Querier struct {
	Engine               Engine `yaml:"engine"`
	ExtraQueryDelay      string `yaml:"extra_query_delay"`
	QueryIngestersWithin string `yaml:"query_ingesters_within"`
	QueryTimeout         string `yaml:"query_timeout"`
	TailMaxDuration      string `yaml:"tail_max_duration"`
}

type Cache struct {
	MemcachedClient MemcachedClient `yaml:"memcached_client"`
}

type ResultsCache struct {
	Cache Cache `yaml:"cache"`
}

type QueryRange struct {
	AlignQueriesWithStep   bool         `yaml:"align_queries_with_step"`
	CacheResults           bool         `yaml:"cache_results"`
	MaxRetries             int          `yaml:"max_retries"`
	ResultsCache           ResultsCache `yaml:"results_cache"`
	SplitQueriesByInterval string       `yaml:"split_queries_by_interval"`
}

type Index struct {
	Period string `yaml:"period"`
	Prefix string `yaml:"prefix"`
}

type Configs struct {
	From        string `yaml:"from"`
	Index       Index  `yaml:"index"`
	ObjectStore string `yaml:"object_store"`
	Schema      string `yaml:"schema"`
	Store       string `yaml:"store"`
}

type SchemaConfig struct {
	Configs []Configs `yaml:"configs"`
}

type Server struct {
	GracefulShutdownTimeout        string `yaml:"graceful_shutdown_timeout"`
	GrpcServerMaxConcurrentStreams int    `yaml:"grpc_server_max_concurrent_streams"`
	GrpcServerMaxRecvMsgSize       int    `yaml:"grpc_server_max_recv_msg_size"`
	GrpcServerMaxSendMsgSize       int    `yaml:"grpc_server_max_send_msg_size"`
	HTTPListenPort                 int    `yaml:"http_listen_port"`
	HTTPServerIdleTimeout          string `yaml:"http_server_idle_timeout"`
	HTTPServerWriteTimeout         string `yaml:"http_server_write_timeout"`
}

type BoltdbShipper struct {
	ActiveIndexDirectory string `yaml:"active_index_directory"`
	CacheLocation        string `yaml:"cache_location"`
	CacheTTL             string `yaml:"cache_ttl"`
	ResyncInterval       string `yaml:"resync_interval"`
	SharedStore          string `yaml:"shared_store"`
}

type IndexQueriesCacheConfig struct {
	Memcached       Memcached       `yaml:"memcached"`
	MemcachedClient MemcachedClient `yaml:"memcached_client"`
}

type StorageConfig struct {
	BoltdbShipper           BoltdbShipper           `yaml:"boltdb_shipper"`
	IndexQueriesCacheConfig IndexQueriesCacheConfig `yaml:"index_queries_cache_config"`
}

type Tracing struct {
	Enabled bool `yaml:"enabled"`
}

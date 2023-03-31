// SPDX-License-Identifier: AGPL-3.0-only

package parse

import (
	"reflect"

	"github.com/grafana/dskit/crypto/tls"
	"github.com/grafana/dskit/grpcclient"
	"github.com/grafana/dskit/kv/consul"
	"github.com/grafana/dskit/kv/etcd"
	"github.com/grafana/dskit/runtimeconfig"
	"github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/distributor"
	"github.com/grafana/loki/pkg/ingester"
	ingester_client "github.com/grafana/loki/pkg/ingester/client"
	"github.com/grafana/loki/pkg/loki/common"
	frontend "github.com/grafana/loki/pkg/lokifrontend"
	"github.com/grafana/loki/pkg/querier"
	"github.com/grafana/loki/pkg/querier/queryrange"
	querier_worker "github.com/grafana/loki/pkg/querier/worker"
	"github.com/grafana/loki/pkg/ruler"
	"github.com/grafana/loki/pkg/scheduler"
	"github.com/grafana/loki/pkg/storage"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client/alibaba"
	"github.com/grafana/loki/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/pkg/storage/chunk/client/azure"
	"github.com/grafana/loki/pkg/storage/chunk/client/baidubce"
	"github.com/grafana/loki/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/pkg/storage/chunk/client/ibmcloud"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/chunk/client/openstack"
	storage_config "github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/compactor"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway"
	"github.com/grafana/loki/pkg/tracing"
	"github.com/grafana/loki/pkg/usagestats"
	"github.com/grafana/loki/pkg/validation"
)

var (
	// RootBlocks is an ordered list of root blocks with their associated descriptions.
	// The order is the same order that will follow the markdown generation.
	// Root blocks map to the configuration variables defined in Config of pkg/loki/loki.go
	RootBlocks = []RootBlock{
		{
			Name:       "server",
			StructType: reflect.TypeOf(server.Config{}),
			Desc:       "Configures the server of the launched module(s).",
		},
		{
			Name:       "distributor",
			StructType: reflect.TypeOf(distributor.Config{}),
			Desc:       "Configures the distributor.",
		},
		{
			Name:       "querier",
			StructType: reflect.TypeOf(querier.Config{}),
			Desc:       "Configures the querier. Only appropriate when running all modules or just the querier.",
		},
		{
			Name:       "query_scheduler",
			StructType: reflect.TypeOf(scheduler.Config{}),
			Desc:       "The query_scheduler block configures the Loki query scheduler. When configured it separates the tenant query queues from the query-frontend.",
		},
		{
			Name:       "frontend",
			StructType: reflect.TypeOf(frontend.Config{}),
			Desc:       "The frontend block configures the Loki query-frontend.",
		},
		{
			Name:       "query_range",
			StructType: reflect.TypeOf(queryrange.Config{}),
			Desc:       "The query_range block configures the query splitting and caching in the Loki query-frontend.",
		},
		{
			Name:       "ruler",
			StructType: reflect.TypeOf(ruler.Config{}),
			Desc:       "The ruler block configures the Loki ruler.",
		},
		{
			Name:       "ingester_client",
			StructType: reflect.TypeOf(ingester_client.Config{}),
			Desc:       "The ingester_client block configures how the distributor will connect to ingesters. Only appropriate when running all components, the distributor, or the querier.",
		},
		{
			Name:       "ingester",
			StructType: reflect.TypeOf(ingester.Config{}),
			Desc:       "The ingester block configures the ingester and how the ingester will register itself to a key value store.",
		},
		{
			Name:       "index_gateway",
			StructType: reflect.TypeOf(indexgateway.Config{}),
			Desc:       "The index_gateway block configures the Loki index gateway server, responsible for serving index queries without the need to constantly interact with the object store.",
		},
		{
			Name:       "storage_config",
			StructType: reflect.TypeOf(storage.Config{}),
			Desc:       "The storage_config block configures one of many possible stores for both the index and chunks. Which configuration to be picked should be defined in schema_config block.",
		},
		{
			Name:       "chunk_store_config",
			StructType: reflect.TypeOf(storage_config.ChunkStoreConfig{}),
			Desc:       "The chunk_store_config block configures how chunks will be cached and how long to wait before saving them to the backing store.",
		},
		{
			Name:       "schema_config",
			StructType: reflect.TypeOf(storage_config.SchemaConfig{}),
			Desc:       "Configures the chunk index schema and where it is stored.",
		},
		{
			Name:       "compactor",
			StructType: reflect.TypeOf(compactor.Config{}),
			Desc:       "The compactor block configures the compactor component, which compacts index shards for performance.",
		},
		{
			Name:       "limits_config",
			StructType: reflect.TypeOf(validation.Limits{}),
			Desc:       "The limits_config block configures global and per-tenant limits in Loki.",
		},
		{
			Name:       "frontend_worker",
			StructType: reflect.TypeOf(querier_worker.Config{}),
			Desc:       "The frontend_worker configures the worker - running within the Loki querier - picking up and executing queries enqueued by the query-frontend.",
		},
		{
			Name:       "table_manager",
			StructType: reflect.TypeOf(index.TableManagerConfig{}),
			Desc:       "The table_manager block configures the table manager for retention.",
		},

		{
			Name:       "runtime_config",
			StructType: reflect.TypeOf(runtimeconfig.Config{}),
			Desc:       "Configuration for 'runtime config' module, responsible for reloading runtime configuration file.",
		},
		{
			Name:       "tracing",
			StructType: reflect.TypeOf(tracing.Config{}),
			Desc:       "Configuration for tracing.",
		},
		{
			Name:       "analytics",
			StructType: reflect.TypeOf(usagestats.Config{}),
			Desc:       "Configuration for usage report.",
		},

		{
			Name:       "common",
			StructType: reflect.TypeOf(common.Config{}),
			Desc:       "Common configuration to be shared between multiple modules. If a more specific configuration is given in other sections, the related configuration within this section will be ignored.",
		},

		// Non-root blocks
		// StoreConfig dskit type: https://github.com/grafana/dskit/blob/main/kv/client.go#L44-L52
		{
			Name:       "consul",
			StructType: reflect.TypeOf(consul.Config{}),
			Desc:       "Configuration for a Consul client. Only applies if store is consul.",
		},
		{
			Name:       "etcd",
			StructType: reflect.TypeOf(etcd.Config{}),
			Desc:       "Configuration for an ETCD v3 client. Only applies if store is etcd.",
		},
		// GRPC client
		{
			Name:       "grpc_client",
			StructType: reflect.TypeOf(grpcclient.Config{}),
			Desc:       "The grpc_client block configures the gRPC client used to communicate between two Loki components.",
		},
		// TLS config
		{
			Name:       "tls_config",
			StructType: reflect.TypeOf(tls.ClientConfig{}),
			Desc:       "The TLS configuration.",
		},
		// Cache config
		{
			Name:       "cache_config",
			StructType: reflect.TypeOf(cache.Config{}),
			Desc:       "The cache block configures the cache backend.",
		},
		// Schema periodic config
		{
			Name:       "period_config",
			StructType: reflect.TypeOf(storage_config.PeriodConfig{}),
			Desc:       "The period_config block configures what index schemas should be used for from specific time periods.",
		},

		// Storage config
		{
			Name:       "aws_storage_config",
			StructType: reflect.TypeOf(aws.StorageConfig{}),
			Desc:       "The aws_storage_config block configures the connection to dynamoDB and S3 object storage. Either one of them or both can be configured.",
		},
		{
			Name:       "azure_storage_config",
			StructType: reflect.TypeOf(azure.BlobStorageConfig{}),
			Desc:       "The azure_storage_config block configures the connection to Azure object storage backend.",
		},
		{
			Name:       "alibabacloud_storage_config",
			StructType: reflect.TypeOf(alibaba.OssConfig{}),
			Desc:       "The alibabacloud_storage_config block configures the connection to Alibaba Cloud Storage object storage backend.",
		},
		{
			Name:       "gcs_storage_config",
			StructType: reflect.TypeOf(gcp.GCSConfig{}),
			Desc:       "The gcs_storage_config block configures the connection to Google Cloud Storage object storage backend.",
		},
		{
			Name:       "s3_storage_config",
			StructType: reflect.TypeOf(aws.S3Config{}),
			Desc:       "The s3_storage_config block configures the connection to Amazon S3 object storage backend.",
		},
		{
			Name:       "bos_storage_config",
			StructType: reflect.TypeOf(baidubce.BOSStorageConfig{}),
			Desc:       "The bos_storage_config block configures the connection to Baidu Object Storage (BOS) object storage backend.",
		},
		{
			Name:       "swift_storage_config",
			StructType: reflect.TypeOf(openstack.SwiftConfig{}),
			Desc:       "The swift_storage_config block configures the connection to OpenStack Object Storage (Swift) object storage backend.",
		},
		{
			Name:       "cos_storage_config",
			StructType: reflect.TypeOf(ibmcloud.COSConfig{}),
			Desc:       "The cos_storage_config block configures the connection to IBM Cloud Object Storage (COS) backend.",
		},
		{
			Name:       "local_storage_config",
			StructType: reflect.TypeOf(local.FSConfig{}),
			Desc:       "The local_storage_config block configures the usage of local file system as object storage backend.",
		},
		{
			Name:       "named_stores_config",
			StructType: reflect.TypeOf(storage.NamedStores{}),
			Desc: `Configures additional object stores for a given storage provider.
Supported stores: aws, azure, bos, filesystem, gcs, swift.
Example:
storage_config:
  named_stores:
    aws:
      store-1:
        endpoint: s3://foo-bucket
        region: us-west1
Named store from this example can be used by setting object_store to store-1 in period_config.`},
	}
)

package distributor

import (
	"path"

	"github.com/ViaQ/logerr/kverrors"
	"github.com/openshift/loki-operator/internal/lokidto"
	"github.com/openshift/loki-operator/internal/manifests"
	"github.com/openshift/loki-operator/internal/manifests/gossip"
	"gopkg.in/yaml.v2"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newConfigMap(namespace, name string) (*core.ConfigMap, error) {
	config := newLokiConfig(namespace)
	c, err := yaml.Marshal(config)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed to marshal config", "config", config)
	}

	return &core.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: apps.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    commonLabels,
		},
		Data: map[string]string{
			"/etc/loki/config/config.yaml": string(c),
		},
	}, nil
}

func newLokiConfig(namespace string) lokidto.Config {
	return lokidto.Config{
		AuthEnabled: true,
		ChunkStoreConfig: &lokidto.ChunkStoreConfig{
			ChunkCacheConfig: &lokidto.CacheConfig{
				FIFOCache: &lokidto.FIFOCache{
					MaxSizeBytes: "1Gi",
				},
			},
		},
		Distributor: &lokidto.Distributor{
			Ring: lokidto.Ring{
				Kvstore: &lokidto.Kvstore{
					Store: "memberlist",
				},
			},
		},
		IngesterClient: &lokidto.IngesterClient{
			GrpcClientConfig: &lokidto.GrpcClientConfig{
				MaxRecvMsgSize: 67108864,
			},
			RemoteTimeout: "1s",
		},
		LimitsConfig: &lokidto.LimitsConfig{
			IngestionBurstSizeMb:      20,
			IngestionRateMb:           10,
			IngestionRateStrategy:     "global",
			MaxCacheFreshnessPerQuery: "10m",
			MaxGlobalStreamsPerUser:   25000,
			MaxQueryLength:            "12000h",
			MaxQueryParallelism:       32,
			RejectOldSamples:          true,
			RejectOldSamplesMaxAge:    "24h",
		},
		Memberlist: &lokidto.Memberlist{
			AbortIfClusterJoinFails: false,
			BindPort:                manifests.GossipPort,
			JoinMembers:             []string{gossip.RingServiceURL(namespace)},
			MaxJoinBackoff:          "1m",
			MaxJoinRetries:          10,
			MinJoinBackoff:          "1s",
		},
		SchemaConfig: &lokidto.SchemaConfig{
			Configs: []lokidto.Configs{
				{
					From: "2020-10-01",
					Index: &lokidto.Index{
						Period: "24h",
						Prefix: "index_",
					},
					ObjectStore: "filesystem",
					Schema:      "v11",
					Store:       "boltdb",
				},
			},
		},
		Server: &lokidto.Server{
			GracefulShutdownTimeout:        "5s",
			GrpcServerMaxConcurrentStreams: 1000,
			GrpcServerMaxRecvMsgSize:       104857600,
			GrpcServerMaxSendMsgSize:       104857600,
			HTTPListenPort:                 3100,
			HTTPServerIdleTimeout:          "120s",
			HTTPServerWriteTimeout:         "1m",
		},
		StorageConfig: &lokidto.StorageConfig{
			BoltDB: &lokidto.BoltDB{
				Directory: path.Join(dataPath, "/index"),
			},
			Filesystem: &lokidto.Filesystem{
				Directory: path.Join(dataPath, "/chunks"),
			},
		},
		Tracing: &lokidto.Tracing{
			Enabled: false,
		},
	}
}

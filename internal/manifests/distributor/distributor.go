package distributor

import (
	"fmt"

	"github.com/openshift/loki-operator/internal/manifests"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

const (
	configVolume = "config"
	dataVolume   = "storage"
)

var (
	commonLabels = map[string]string{
		"app.kubernetes.io/provider":  "openshift",
		"app.kubernetes.io/component": "distributor",
	}

	gossipLabels = labels.Merge(commonLabels, map[string]string{
		"loki.grafana.com/gossip":     "true",
	})
)

// New creates a new set of distributor objects to be deployed
func New(namespace, name string) *Distributor {
	return &Distributor{
		deployment: newDeployment(namespace, fmt.Sprintf("%s-distributor", name)),
		configMaps: newConfigMap(namespace, fmt.Sprintf("%s-distributor", name)),
	}
}

// Distributor is a collection of manifests for deploying a working Loki Distributor.
type Distributor struct {
	deployment *apps.Deployment
	configMap  *core.ConfigMap
}

func newConfigMap(namespace, name string) *core.ConfigMap {
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
			"/etc/loki/config/config.yaml": "",
		},
	}
}

const cm = `
  "auth_enabled": true
  "chunk_store_config":
	"chunk_cache_config":
	  "memcached":
		"batch_size": 100
		"parallelism": 100
	  "memcached_client":
		"addresses": "dns+observatorium-loki-chunk-cache.${NAMESPACE}.svc.cluster.local:11211"
		"consistent_hash": true
		"max_idle_conns": 100
		"timeout": "100ms"
		"update_interval": "1m"
	"max_look_back_period": "0s"
  "compactor":
	"compaction_interval": "2h"
	"shared_store": "s3"
	"working_directory": "/data/loki/compactor"
  "distributor":
	"ring":
	  "kvstore":
		"store": "memberlist"
  "frontend":
	"compress_responses": true
	"max_outstanding_per_tenant": 200
  "frontend_worker":
	"frontend_address": "observatorium-loki-query-frontend-grpc.${NAMESPACE}.svc.cluster.local:9095"
	"grpc_client_config":
	  "max_send_msg_size": 104857600
	"parallelism": 32
  "ingester":
	"chunk_block_size": 262144
	"chunk_encoding": "snappy"
	"chunk_idle_period": "2h"
	"chunk_retain_period": "1m"
	"chunk_target_size": 1572864
	"lifecycler":
	  "heartbeat_period": "5s"
	  "interface_names":
	  - "eth0"
	  "join_after": "60s"
	  "num_tokens": 512
	  "ring":
		"heartbeat_timeout": "1m"
		"kvstore":
		  "store": "memberlist"
	"max_transfer_retries": 0
  "ingester_client":
	"grpc_client_config":
	  "max_recv_msg_size": 67108864
	"remote_timeout": "1s"
  "limits_config":
	"enforce_metric_name": false
	"ingestion_burst_size_mb": 20
	"ingestion_rate_mb": 10
	"ingestion_rate_strategy": "global"
	"max_cache_freshness_per_query": "10m"
	"max_global_streams_per_user": 25000
	"max_query_length": "12000h"
	"max_query_parallelism": 32
	"max_streams_per_user": 0
	"reject_old_samples": true
	"reject_old_samples_max_age": "24h"
  "memberlist":
	"abort_if_cluster_join_fails": false
	"bind_port": 7946
	"join_members":
	- "observatorium-loki-gossip-ring.${NAMESPACE}.svc.cluster.local:7946"
	"max_join_backoff": "1m"
	"max_join_retries": 10
	"min_join_backoff": "1s"
  "querier":
	"engine":
	  "max_look_back_period": "5m"
	  "timeout": "3m"
	"extra_query_delay": "0s"
	"query_ingesters_within": "2h"
	"query_timeout": "1h"
	"tail_max_duration": "1h"
  "query_range":
	"align_queries_with_step": true
	"cache_results": true
	"max_retries": 5
	"results_cache":
	  "cache":
		"memcached_client":
		  "addresses": "dns+observatorium-loki-results-cache.${NAMESPACE}.svc.cluster.local:11211"
		  "consistent_hash": true
		  "max_idle_conns": 16
		  "timeout": "500ms"
		  "update_interval": "1m"
	"split_queries_by_interval": "30m"
  "schema_config":
	"configs":
	- "from": "2020-10-01"
	  "index":
		"period": "24h"
		"prefix": "loki_index_"
	  "object_store": "s3"
	  "schema": "v11"
	  "store": "boltdb-shipper"
  "server":
	"graceful_shutdown_timeout": "5s"
	"grpc_server_max_concurrent_streams": 1000
	"grpc_server_max_recv_msg_size": 104857600
	"grpc_server_max_send_msg_size": 104857600
	"http_listen_port": 3100
	"http_server_idle_timeout": "120s"
	"http_server_write_timeout": "1m"
  "storage_config":
	"boltdb_shipper":
	  "active_index_directory": "/data/loki/index"
	  "cache_location": "/data/loki/index_cache"
	  "cache_ttl": "24h"
	  "resync_interval": "5m"
	  "shared_store": "s3"
	"index_queries_cache_config":
	  "memcached":
		"batch_size": 100
		"parallelism": 100
	  "memcached_client":
		"addresses": "dns+observatorium-loki-index-query-cache.${NAMESPACE}.svc.cluster.local:11211"
		"consistent_hash": true
  "tracing":
	"enabled": true
`

func newDeployment(namespace, name string) *apps.Deployment {
	podSpec := core.PodSpec{
		Volumes: []core.Volume{
			{
				Name: configVolume,
				VolumeSource: core.VolumeSource{
					ConfigMap: &core.ConfigMapVolumeSource{
						LocalObjectReference: core.LocalObjectReference{
							Name: configVolume,
						},
					},
				},
			},
			{
				Name: dataVolume,
				VolumeSource: core.VolumeSource{
					EmptyDir: &core.EmptyDirVolumeSource{},
				},
			},
		},
		Containers: []core.Container{
			{
				Image: manifests.ContainerImage,
				Name:  "loki-distributor",
				Args: []string{
					"-config.file=/etc/loki/config/config.yaml",
					"-distributor.replication-factor=1",
					"-limits.per-user-override-config=/etc/loki/config/overrides.yaml",
					"-log.level=info",
					"-querier.worker-parallelism=1",
					"-target=distributor",
				},
				ReadinessProbe: &core.Probe{
					Handler: core.Handler{
						HTTPGet: &core.HTTPGetAction{
							Path:   "/ready",
							Port:   intstr.FromInt(3100),
							Scheme: core.URISchemeHTTP,
						},
					},
					InitialDelaySeconds: 15,
					TimeoutSeconds:      1,
				},
				LivenessProbe: &core.Probe{
					Handler: core.Handler{
						HTTPGet: &core.HTTPGetAction{
							Path:   "/metrics",
							Port:   intstr.FromInt(3100),
							Scheme: core.URISchemeHTTP,
						},
					},
					TimeoutSeconds:   2,
					PeriodSeconds:    30,
					FailureThreshold: 10,
				},
				Ports: []core.ContainerPort{
					{
						Name:          "metrics",
						ContainerPort: 3100,
					},
					{
						Name:          "grpc",
						ContainerPort: 9095,
					},
					{
						Name:          "gossip-ring",
						ContainerPort: 7946,
					},
				},
				Resources: core.ResourceRequirements{
					Limits: core.ResourceList{
						core.ResourceMemory: resource.MustParse("1Gi"),
						core.ResourceCPU:    resource.MustParse("1000m"),
					},
					Requests: core.ResourceList{
						core.ResourceMemory: resource.MustParse("50m"),
						core.ResourceCPU:    resource.MustParse("50m"),
					},
				},
				VolumeMounts: []core.VolumeMount{
					{
						Name:      configVolume,
						ReadOnly:  false,
						MountPath: "/etc/loki/config",
					},
					{
						Name:      dataVolume,
						ReadOnly:  false,
						MountPath: "/data",
					},
				},
			},
		},
	}

	return &apps.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: apps.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    commonLabels,
		},
		Spec: apps.DeploymentSpec{
			Replicas: pointer.Int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: commonLabels,
			},
			Template: core.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: commonLabels,
				},
				Spec: podSpec,
			},
			Strategy: apps.DeploymentStrategy{
				Type: apps.RollingUpdateDeploymentStrategyType,
			},
		},
	}
}

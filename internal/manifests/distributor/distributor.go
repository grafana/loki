package distributor

import (
	"fmt"

	"github.com/openshift/loki-operator/internal/manifests"
	"github.com/openshift/loki-operator/internal/manifests/gossip"
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
	dataPath     = "/data/loki"
)

var (
	commonLabels = map[string]string{
		"app.kubernetes.io/name":     "loki",
		"app.kubernetes.io/provider": "openshift",
		"loki.grafana.com/component": "distributor",
	}
)

// New creates a new set of distributor objects to be deployed
func New(namespace, name string, replicas int) (*Distributor, error) {
	cm, err := newConfigMap(namespace, fmt.Sprintf("%s-distributor", name))
	if err != nil {
		return nil, err
	}

	return &Distributor{
		deployment: newDeployment(namespace, fmt.Sprintf("%s-distributor", name), replicas),
		configMap:  cm,
		services:   services(namespace),
	}, nil
}

// Distributor is a collection of manifests for deploying a working Loki Distributor.
type Distributor struct {
	deployment *apps.Deployment
	configMap  *core.ConfigMap
	services   []core.Service
}

func (d *Distributor) Deployment() *apps.Deployment {
	return d.deployment
}

func newDeployment(namespace, name string, replicas int) *apps.Deployment {
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
						ContainerPort: manifests.MetricsPort,
					},
					{
						Name:          "grpc",
						ContainerPort: manifests.GRPCPort,
					},
					{
						Name:          "gossip-ring",
						ContainerPort: manifests.GossipPort,
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
						MountPath: dataPath,
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
			Replicas: pointer.Int32Ptr(int32(replicas)),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(commonLabels, gossip.Labels()),
			},
			Template: core.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:   name,
					Labels: labels.Merge(commonLabels, gossip.Labels()),
				},
				Spec: podSpec,
			},
			Strategy: apps.DeploymentStrategy{
				Type: apps.RollingUpdateDeploymentStrategyType,
			},
		},
	}
}

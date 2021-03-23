package manifests

import (
	"fmt"
	"path"

	"github.com/ViaQ/loki-operator/internal/manifests/internal/config"
	apps "k8s.io/api/apps/v1"
	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildIngester builds the k8s objects required to run Loki Ingester
func BuildIngester(stackName string) []client.Object {
	return []client.Object{
		NewIngesterDeployment(stackName),
		NewIngesterGRPCService(stackName),
		NewIngesterHTTPService(stackName),
	}
}

// NewIngesterDeployment creates a deployment object for an ingester
func NewIngesterDeployment(stackName string) *apps.Deployment {
	podSpec := core.PodSpec{
		Volumes: []core.Volume{
			{
				Name: configVolumeName,
				VolumeSource: core.VolumeSource{
					ConfigMap: &core.ConfigMapVolumeSource{
						LocalObjectReference: core.LocalObjectReference{
							Name: lokiConfigMapName(stackName),
						},
					},
				},
			},
			{
				Name: storageVolumeName,
				VolumeSource: core.VolumeSource{
					EmptyDir: &core.EmptyDirVolumeSource{},
				},
			},
		},
		Containers: []core.Container{
			{
				Image: containerImage,
				Name:  "loki-ingester",
				Args: []string{
					"-target=ingester",
					fmt.Sprintf("-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiConfigFileName)),
				},
				ReadinessProbe: &core.Probe{
					Handler: core.Handler{
						HTTPGet: &core.HTTPGetAction{
							Path:   "/ready",
							Port:   intstr.FromInt(httpPort),
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
							Port:   intstr.FromInt(httpPort),
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
						ContainerPort: httpPort,
					},
					{
						Name:          "grpc",
						ContainerPort: grpcPort,
					},
					{
						Name:          "gossip-ring",
						ContainerPort: gossipPort,
					},
				},
				// Resources: core.ResourceRequirements{
				// 	Limits: core.ResourceList{
				// 		core.ResourceMemory: resource.MustParse("1Gi"),
				// 		core.ResourceCPU:    resource.MustParse("1000m"),
				// 	},
				// 	Requests: core.ResourceList{
				// 		core.ResourceMemory: resource.MustParse("50m"),
				// 		core.ResourceCPU:    resource.MustParse("50m"),
				// 	},
				// },
				VolumeMounts: []core.VolumeMount{
					{
						Name:      configVolumeName,
						ReadOnly:  false,
						MountPath: config.LokiConfigMountDir,
					},
					{
						Name:      storageVolumeName,
						ReadOnly:  false,
						MountPath: dataDirectory,
					},
				},
			},
		},
	}

	l := ComponentLabels("ingester", stackName)

	return &apps.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: apps.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("loki-ingester-%s", stackName),
			Labels: l,
		},
		Spec: apps.DeploymentSpec{
			Replicas: pointer.Int32Ptr(int32(3)),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: core.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:   fmt.Sprintf("loki-ingester-%s", stackName),
					Labels: labels.Merge(l, GossipLabels()),
				},
				Spec: podSpec,
			},
			Strategy: apps.DeploymentStrategy{
				Type: apps.RollingUpdateDeploymentStrategyType,
			},
		},
	}
}

// NewIngesterGRPCService creates a k8s service for the ingester GRPC endpoint
func NewIngesterGRPCService(stackName string) *core.Service {
	l := ComponentLabels("ingester", stackName)
	return &core.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: apps.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceNameIngesterGRPC(stackName),
			Labels: l,
		},
		Spec: core.ServiceSpec{
			ClusterIP: "None",
			Ports: []core.ServicePort{
				{
					Name: "grpc",
					Port: grpcPort,
				},
			},
			Selector: l,
		},
	}
}

// NewIngesterHTTPService creates a k8s service for the ingester HTTP endpoint
func NewIngesterHTTPService(stackName string) *core.Service {
	l := ComponentLabels("ingester", stackName)
	return &core.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: apps.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceNameIngesterHTTP(stackName),
			Labels: l,
		},
		Spec: core.ServiceSpec{
			Ports: []core.ServicePort{
				{
					Name: "metrics",
					Port: httpPort,
				},
			},
			Selector: l,
		},
	}
}

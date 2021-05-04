package manifests

import (
	"fmt"
	"path"

	"github.com/ViaQ/loki-operator/internal/manifests/internal"
	"github.com/ViaQ/loki-operator/internal/manifests/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	configVolumeName  = "config"
	storageVolumeName = "storage"
	dataDirectory     = "/tmp/loki"
)

// BuildDistributor returns a list of k8s objects for Loki Distributor
func BuildDistributor(opt Options) []client.Object {
	return []client.Object{
		NewDistributorDeployment(opt),
		NewDistributorHTTPService(opt.Name),
		NewDistributorGRPCService(opt.Name),
	}
}

// NewDistributorDeployment creates a deployment object for a distributor
func NewDistributorDeployment(opt Options) *appsv1.Deployment {
	podSpec := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: configVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: lokiConfigMapName(opt.Name),
						},
					},
				},
			},
			{
				Name: storageVolumeName,
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		},
		Containers: []corev1.Container{
			{
				Image:     opt.Image,
				Name:      "loki-distributor",
				Resources: internal.ResourceSizeTable[opt.Stack.Size].Distributor,
				Args: []string{
					"-target=distributor",
					fmt.Sprintf("-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiConfigFileName)),
				},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "/ready",
							Port:   intstr.FromInt(httpPort),
							Scheme: corev1.URISchemeHTTP,
						},
					},
					InitialDelaySeconds: 15,
					TimeoutSeconds:      1,
				},
				LivenessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "/metrics",
							Port:   intstr.FromInt(httpPort),
							Scheme: corev1.URISchemeHTTP,
						},
					},
					TimeoutSeconds:   2,
					PeriodSeconds:    30,
					FailureThreshold: 10,
				},
				Ports: []corev1.ContainerPort{
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
				VolumeMounts: []corev1.VolumeMount{
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

	if opt.Stack.Template != nil && opt.Stack.Template.Distributor != nil {
		podSpec.Tolerations = opt.Stack.Template.Distributor.Tolerations
		podSpec.NodeSelector = opt.Stack.Template.Distributor.NodeSelector
	}

	l := ComponentLabels("distributor", opt.Name)
	a := commonAnnotations(opt.ConfigSHA1)

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("loki-distributor-%s", opt.Name),
			Labels: l,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32Ptr(opt.Stack.Template.Distributor.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("loki-distributor-%s", opt.Name),
					Labels:      labels.Merge(l, GossipLabels()),
					Annotations: a,
				},
				Spec: podSpec,
			},
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
		},
	}
}

// NewDistributorHTTPService creates a k8s service for the distributor HTTP endpoint
func NewDistributorHTTPService(stackName string) *corev1.Service {
	l := ComponentLabels("distributor", stackName)
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceNameDistributorGRPC(stackName),
			Labels: l,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Ports: []corev1.ServicePort{
				{
					Name: "grpc",
					Port: grpcPort,
				},
			},
			Selector: l,
		},
	}
}

// NewDistributorGRPCService creates a k8s service for the distributor GRPC endpoint
func NewDistributorGRPCService(stackName string) *corev1.Service {
	l := ComponentLabels("distributor", stackName)
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceNameDistributorHTTP(stackName),
			Labels: l,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "metrics",
					Port: httpPort,
				},
			},
			Selector: l,
		},
	}
}

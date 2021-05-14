package manifests

import (
	"fmt"
	"path"

	"github.com/ViaQ/loki-operator/internal/manifests/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildQueryFrontend returns a list of k8s objects for Loki QueryFrontend
func BuildQueryFrontend(opt Options) []client.Object {
	return []client.Object{
		NewQueryFrontendDeployment(opt),
		NewQueryFrontendGRPCService(opt.Name),
		NewQueryFrontendHTTPService(opt.Name),
	}
}

// NewQueryFrontendDeployment creates a deployment object for a query-frontend
func NewQueryFrontendDeployment(opt Options) *appsv1.Deployment {
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
				Image: opt.Image,
				Name:  "loki-query-frontend",
				Resources: corev1.ResourceRequirements{
					Limits:   opt.ResourceRequirements.QueryFrontend.Limits,
					Requests: opt.ResourceRequirements.QueryFrontend.Requests,
				},
				Args: []string{
					"-target=query-frontend",
					fmt.Sprintf("-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiConfigFileName)),
					fmt.Sprintf("-runtime-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiRuntimeConfigFileName)),
				},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "/metrics",
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

	if opt.Stack.Template != nil && opt.Stack.Template.QueryFrontend != nil {
		podSpec.Tolerations = opt.Stack.Template.QueryFrontend.Tolerations
		podSpec.NodeSelector = opt.Stack.Template.QueryFrontend.NodeSelector
	}

	l := ComponentLabels("query-frontend", opt.Name)
	a := commonAnnotations(opt.ConfigSHA1)

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("loki-query-frontend-%s", opt.Name),
			Labels: l,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32Ptr(opt.Stack.Template.QueryFrontend.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("loki-query-frontend-%s", opt.Name),
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

// NewQueryFrontendGRPCService creates a k8s service for the querier GRPC endpoint
func NewQueryFrontendGRPCService(stackName string) *corev1.Service {
	l := ComponentLabels("query-frontend", stackName)
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("loki-query-frontend-grpc-%s", stackName),
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

// NewQueryFrontendHTTPService creates a k8s service for the querier HTTP endpoint
func NewQueryFrontendHTTPService(stackName string) *corev1.Service {
	l := ComponentLabels("query-frontend", stackName)
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("loki-query-frontend-http-%s", stackName),
			Labels: l,
		},
		Spec: corev1.ServiceSpec{
			Ports: []corev1.ServicePort{
				{
					Name: "http",
					Port: httpPort,
				},
			},
			Selector: l,
		},
	}
}

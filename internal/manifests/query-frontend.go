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
func BuildQueryFrontend(opts Options) ([]client.Object, error) {
	deployment := NewQueryFrontendDeployment(opts)
	if opts.Flags.EnableTLSServiceMonitorConfig {
		if err := configureQueryFrontendServiceMonitorPKI(deployment, opts.Name); err != nil {
			return nil, err
		}
	}

	return []client.Object{
		deployment,
		NewQueryFrontendGRPCService(opts),
		NewQueryFrontendHTTPService(opts),
	}, nil
}

// NewQueryFrontendDeployment creates a deployment object for a query-frontend
func NewQueryFrontendDeployment(opts Options) *appsv1.Deployment {
	podSpec := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: configVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: lokiConfigMapName(opts.Name),
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
				Image: opts.Image,
				Name:  "loki-query-frontend",
				Resources: corev1.ResourceRequirements{
					Limits:   opts.ResourceRequirements.QueryFrontend.Limits,
					Requests: opts.ResourceRequirements.QueryFrontend.Requests,
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

	if opts.Stack.Template != nil && opts.Stack.Template.QueryFrontend != nil {
		podSpec.Tolerations = opts.Stack.Template.QueryFrontend.Tolerations
		podSpec.NodeSelector = opts.Stack.Template.QueryFrontend.NodeSelector
	}

	l := ComponentLabels(LabelQueryFrontendComponent, opts.Name)
	a := commonAnnotations(opts.ConfigSHA1)

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   QueryFrontendName(opts.Name),
			Labels: l,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32Ptr(opts.Stack.Template.QueryFrontend.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("loki-query-frontend-%s", opts.Name),
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

// NewQueryFrontendGRPCService creates a k8s service for the query-frontend GRPC endpoint
func NewQueryFrontendGRPCService(opts Options) *corev1.Service {
	l := ComponentLabels(LabelQueryFrontendComponent, opts.Name)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceNameQueryFrontendGRPC(opts.Name),
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

// NewQueryFrontendHTTPService creates a k8s service for the query-frontend HTTP endpoint
func NewQueryFrontendHTTPService(opts Options) *corev1.Service {
	serviceName := serviceNameQueryFrontendHTTP(opts.Name)
	l := ComponentLabels(LabelQueryFrontendComponent, opts.Name)
	a := serviceAnnotations(serviceName, opts.Flags.EnableCertificateSigningService)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Labels:      l,
			Annotations: a,
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

func configureQueryFrontendServiceMonitorPKI(deployment *appsv1.Deployment, stackName string) error {
	serviceName := serviceNameQueryFrontendHTTP(stackName)
	return configureServiceMonitorPKI(&deployment.Spec.Template.Spec, serviceName)
}

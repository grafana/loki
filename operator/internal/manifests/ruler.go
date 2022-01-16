package manifests

import (
	"fmt"
	"path"

	"github.com/grafana/loki-operator/internal/manifests/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	rulesVolumeName    = "rules"
	rulesConfigmapName = "rules"
	rulesMountDir      = "/etc/loki/rules"
)

// BuildRuler returns a list of k8s objects for Loki Ruler
func BuildRuler(opts Options) ([]client.Object, error) {
	deployment := NewRulerDeployment(opts)
	if opts.Flags.EnableTLSServiceMonitorConfig {
		if err := configureRulerServiceMonitorPKI(deployment, opts.Name); err != nil {
			return nil, err
		}
	}

	return []client.Object{
		deployment,
		NewRulerGRPCService(opts),
		NewRulerHTTPService(opts),
	}, nil
}

// NewRulerDeployment creates a deployment object for a ruler
func NewRulerDeployment(opts Options) *appsv1.Deployment {
	podSpec := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: rulesVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: rulesConfigmapName,
						},
					},
				},
			},
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
				Name:  "loki-ruler",
				Resources: corev1.ResourceRequirements{
					Limits:   opts.ResourceRequirements.Ruler.Limits,
					Requests: opts.ResourceRequirements.Ruler.Requests,
				},
				Args: []string{
					"-target=ruler",
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
						Name:      rulesVolumeName,
						ReadOnly:  false,
						MountPath: rulesMountDir,
					},
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

	if opts.Stack.Template != nil && opts.Stack.Template.Ruler != nil {
		podSpec.Tolerations = opts.Stack.Template.Ruler.Tolerations
		podSpec.NodeSelector = opts.Stack.Template.Ruler.NodeSelector
	}

	l := ComponentLabels(LabelRulerComponent, opts.Name)
	a := commonAnnotations(opts.ConfigSHA1)

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   RulerName(opts.Name),
			Labels: l,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32Ptr(opts.Stack.Template.Ruler.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("loki-ruler-%s", opts.Name),
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

// NewRulerGRPCService creates a k8s service for the ruler GRPC endpoint
func NewRulerGRPCService(opts Options) *corev1.Service {
	l := ComponentLabels(LabelRulerComponent, opts.Name)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceNameRulerGRPC(opts.Name),
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

// NewRulerHTTPService creates a k8s service for the ruler HTTP endpoint
func NewRulerHTTPService(opts Options) *corev1.Service {
	serviceName := serviceNameRulerHTTP(opts.Name)
	l := ComponentLabels(LabelRulerComponent, opts.Name)
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

func configureRulerServiceMonitorPKI(deployment *appsv1.Deployment, stackName string) error {
	serviceName := serviceNameRulerHTTP(stackName)
	return configureServiceMonitorPKI(&deployment.Spec.Template.Spec, serviceName)
}

package manifests

import (
	"fmt"
	"path"

	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	walVolumeName          = "wal"
	configVolumeName       = "config"
	rulesStorageVolumeName = "rules"
	storageVolumeName      = "storage"
	walDirectory           = "/tmp/wal"
	dataDirectory          = "/tmp/loki"
	rulesStorageDirectory  = "/tmp/rules"
	secretDirectory        = "/etc/proxy/secrets"
)

// BuildDistributor returns a list of k8s objects for Loki Distributor
func BuildDistributor(opts Options) ([]client.Object, error) {
	deployment := NewDistributorDeployment(opts)
	if opts.Flags.EnableTLSServiceMonitorConfig {
		if err := configureDistributorServiceMonitorPKI(deployment, opts.Name); err != nil {
			return nil, err
		}
	}

	return []client.Object{
		deployment,
		NewDistributorGRPCService(opts),
		NewDistributorHTTPService(opts),
	}, nil
}

// NewDistributorDeployment creates a deployment object for a distributor
func NewDistributorDeployment(opts Options) *appsv1.Deployment {
	podSpec := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: configVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						DefaultMode: &defaultConfigMapMode,
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
				Name:  "loki-distributor",
				Resources: corev1.ResourceRequirements{
					Limits:   opts.ResourceRequirements.Distributor.Limits,
					Requests: opts.ResourceRequirements.Distributor.Requests,
				},
				Args: []string{
					"-target=distributor",
					fmt.Sprintf("-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiConfigFileName)),
					fmt.Sprintf("-runtime-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiRuntimeConfigFileName)),
				},
				ReadinessProbe: lokiReadinessProbe(),
				LivenessProbe:  lokiLivenessProbe(),
				Ports: []corev1.ContainerPort{
					{
						Name:          lokiHTTPPortName,
						ContainerPort: httpPort,
						Protocol:      protocolTCP,
					},
					{
						Name:          lokiGRPCPortName,
						ContainerPort: grpcPort,
						Protocol:      protocolTCP,
					},
					{
						Name:          lokiGossipPortName,
						ContainerPort: gossipPort,
						Protocol:      protocolTCP,
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
				TerminationMessagePath:   "/dev/termination-log",
				TerminationMessagePolicy: "File",
				ImagePullPolicy:          "IfNotPresent",
			},
		},
	}

	if opts.Stack.Template != nil && opts.Stack.Template.Distributor != nil {
		podSpec.Tolerations = opts.Stack.Template.Distributor.Tolerations
		podSpec.NodeSelector = opts.Stack.Template.Distributor.NodeSelector
	}

	l := ComponentLabels(LabelDistributorComponent, opts.Name)
	a := commonAnnotations(opts.ConfigSHA1)

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   DistributorName(opts.Name),
			Labels: l,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32Ptr(opts.Stack.Template.Distributor.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("loki-distributor-%s", opts.Name),
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

// NewDistributorGRPCService creates a k8s service for the distributor GRPC endpoint
func NewDistributorGRPCService(opts Options) *corev1.Service {
	l := ComponentLabels(LabelDistributorComponent, opts.Name)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceNameDistributorGRPC(opts.Name),
			Labels: l,
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Ports: []corev1.ServicePort{
				{
					Name:       lokiGRPCPortName,
					Port:       grpcPort,
					Protocol:   protocolTCP,
					TargetPort: intstr.IntOrString{IntVal: grpcPort},
				},
			},
			Selector: l,
		},
	}
}

// NewDistributorHTTPService creates a k8s service for the distributor HTTP endpoint
func NewDistributorHTTPService(opts Options) *corev1.Service {
	serviceName := serviceNameDistributorHTTP(opts.Name)
	l := ComponentLabels(LabelDistributorComponent, opts.Name)
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
					Name:       lokiHTTPPortName,
					Port:       httpPort,
					Protocol:   protocolTCP,
					TargetPort: intstr.IntOrString{IntVal: httpPort},
				},
			},
			Selector: l,
		},
	}
}

func configureDistributorServiceMonitorPKI(deployment *appsv1.Deployment, stackName string) error {
	serviceName := serviceNameDistributorHTTP(stackName)
	return configureServiceMonitorPKI(&deployment.Spec.Template.Spec, serviceName)
}

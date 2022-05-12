package manifests

import (
	"fmt"
	"path"

	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildIndexGateway returns a list of k8s objects for Loki IndexGateway
func BuildIndexGateway(opts Options) ([]client.Object, error) {
	statefulSet := NewIndexGatewayStatefulSet(opts)
	if opts.Flags.EnableTLSServiceMonitorConfig {
		if err := configureIndexGatewayServiceMonitorPKI(statefulSet, opts.Name); err != nil {
			return nil, err
		}
	}

	storageType := opts.Stack.Storage.Secret.Type
	secretName := opts.Stack.Storage.Secret.Name
	if err := configureStatefulSetForStorageType(statefulSet, storageType, secretName); err != nil {
		return nil, err
	}

	return []client.Object{
		statefulSet,
		NewIndexGatewayGRPCService(opts),
		NewIndexGatewayHTTPService(opts),
	}, nil
}

// NewIndexGatewayStatefulSet creates a statefulset object for an index-gateway
func NewIndexGatewayStatefulSet(opts Options) *appsv1.StatefulSet {
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
		},
		Containers: []corev1.Container{
			{
				Image: opts.Image,
				Name:  "loki-index-gateway",
				Resources: corev1.ResourceRequirements{
					Limits:   opts.ResourceRequirements.IndexGateway.Limits,
					Requests: opts.ResourceRequirements.IndexGateway.Requests,
				},
				Args: []string{
					"-target=index-gateway",
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

	if opts.Stack.Template != nil && opts.Stack.Template.IndexGateway != nil {
		podSpec.Tolerations = opts.Stack.Template.IndexGateway.Tolerations
		podSpec.NodeSelector = opts.Stack.Template.IndexGateway.NodeSelector
	}

	l := ComponentLabels(LabelIndexGatewayComponent, opts.Name)
	a := commonAnnotations(opts.ConfigSHA1)

	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   IndexGatewayName(opts.Name),
			Labels: l,
		},
		Spec: appsv1.StatefulSetSpec{
			PodManagementPolicy:  appsv1.OrderedReadyPodManagement,
			RevisionHistoryLimit: pointer.Int32Ptr(10),
			Replicas:             pointer.Int32Ptr(opts.Stack.Template.IndexGateway.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("loki-index-gateway-%s", opts.Name),
					Labels:      labels.Merge(l, GossipLabels()),
					Annotations: a,
				},
				Spec: podSpec,
			},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{
					ObjectMeta: metav1.ObjectMeta{
						Labels: l,
						Name:   storageVolumeName,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							// TODO: should we verify that this is possible with the given storage class first?
							corev1.ReadWriteOnce,
						},
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: opts.ResourceRequirements.IndexGateway.PVCSize,
							},
						},
						StorageClassName: pointer.StringPtr(opts.Stack.StorageClassName),
						VolumeMode:       &volumeFileSystemMode,
					},
				},
			},
		},
	}
}

// NewIndexGatewayGRPCService creates a k8s service for the index-gateway GRPC endpoint
func NewIndexGatewayGRPCService(opts Options) *corev1.Service {
	l := ComponentLabels(LabelIndexGatewayComponent, opts.Name)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceNameIndexGatewayGRPC(opts.Name),
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

// NewIndexGatewayHTTPService creates a k8s service for the index-gateway HTTP endpoint
func NewIndexGatewayHTTPService(opts Options) *corev1.Service {
	serviceName := serviceNameIndexGatewayHTTP(opts.Name)
	l := ComponentLabels(LabelIndexGatewayComponent, opts.Name)
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

func configureIndexGatewayServiceMonitorPKI(statefulSet *appsv1.StatefulSet, stackName string) error {
	serviceName := serviceNameIndexGatewayHTTP(stackName)
	return configureServiceMonitorPKI(&statefulSet.Spec.Template.Spec, serviceName)
}

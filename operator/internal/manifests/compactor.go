package manifests

import (
	"fmt"
	"path"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

// BuildCompactor builds the k8s objects required to run Loki Compactor.
func BuildCompactor(opts Options) ([]client.Object, error) {
	statefulSet := NewCompactorStatefulSet(opts)
	if opts.Gates.HTTPEncryption {
		if err := configureCompactorHTTPServicePKI(statefulSet, opts); err != nil {
			return nil, err
		}
	}

	if err := storage.ConfigureStatefulSet(statefulSet, opts.ObjectStorage); err != nil {
		return nil, err
	}

	if opts.Gates.GRPCEncryption {
		if err := configureCompactorGRPCServicePKI(statefulSet, opts); err != nil {
			return nil, err
		}
	}

	if opts.Gates.HTTPEncryption || opts.Gates.GRPCEncryption {
		caBundleName := signingCABundleName(opts.Name)
		if err := configureServiceCA(&statefulSet.Spec.Template.Spec, caBundleName); err != nil {
			return nil, err
		}
	}

	if opts.Gates.RestrictedPodSecurityStandard {
		if err := configurePodSpecForRestrictedStandard(&statefulSet.Spec.Template.Spec); err != nil {
			return nil, err
		}
	}

	if err := configureHashRingEnv(&statefulSet.Spec.Template.Spec, opts); err != nil {
		return nil, err
	}

	if err := configureProxyEnv(&statefulSet.Spec.Template.Spec, opts); err != nil {
		return nil, err
	}

	return []client.Object{
		statefulSet,
		NewCompactorGRPCService(opts),
		NewCompactorHTTPService(opts),
	}, nil
}

// NewCompactorStatefulSet creates a statefulset object for a compactor.
func NewCompactorStatefulSet(opts Options) *appsv1.StatefulSet {
	l := ComponentLabels(LabelCompactorComponent, opts.Name)
	a := commonAnnotations(opts)
	podSpec := corev1.PodSpec{
		ServiceAccountName: opts.Name,
		Affinity:           configureAffinity(LabelCompactorComponent, opts.Name, opts.Gates.DefaultNodeAffinity, opts.Stack.Template.Compactor),
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
				Name:  "loki-compactor",
				Resources: corev1.ResourceRequirements{
					Limits:   opts.ResourceRequirements.Compactor.Limits,
					Requests: opts.ResourceRequirements.Compactor.Requests,
				},
				Args: []string{
					"-target=compactor",
					fmt.Sprintf("-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiConfigFileName)),
					fmt.Sprintf("-runtime-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiRuntimeConfigFileName)),
					"-config.expand-env=true",
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

	if opts.Stack.Template != nil && opts.Stack.Template.Compactor != nil {
		podSpec.Tolerations = opts.Stack.Template.Compactor.Tolerations
		podSpec.NodeSelector = opts.Stack.Template.Compactor.NodeSelector
	}

	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   CompactorName(opts.Name),
			Labels: l,
		},
		Spec: appsv1.StatefulSetSpec{
			PodManagementPolicy:  appsv1.OrderedReadyPodManagement,
			RevisionHistoryLimit: ptr.To(defaultRevHistoryLimit),
			Replicas:             ptr.To(opts.Stack.Template.Compactor.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("loki-compactor-%s", opts.Name),
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
						Resources: corev1.VolumeResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: opts.ResourceRequirements.Compactor.PVCSize,
							},
						},
						VolumeMode:       &volumeFileSystemMode,
						StorageClassName: ptr.To(opts.Stack.StorageClassName),
					},
				},
			},
		},
	}
}

// NewCompactorGRPCService creates a k8s service for the compactor GRPC endpoint
func NewCompactorGRPCService(opts Options) *corev1.Service {
	serviceName := serviceNameCompactorGRPC(opts.Name)
	labels := ComponentLabels(LabelCompactorComponent, opts.Name)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceName,
			Labels: labels,
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
			Selector: labels,
		},
	}
}

// NewCompactorHTTPService creates a k8s service for the ingester HTTP endpoint
func NewCompactorHTTPService(opts Options) *corev1.Service {
	serviceName := serviceNameCompactorHTTP(opts.Name)
	labels := ComponentLabels(LabelCompactorComponent, opts.Name)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   serviceName,
			Labels: labels,
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
			Selector: labels,
		},
	}
}

func configureCompactorHTTPServicePKI(statefulSet *appsv1.StatefulSet, opts Options) error {
	serviceName := serviceNameCompactorHTTP(opts.Name)
	return configureHTTPServicePKI(&statefulSet.Spec.Template.Spec, serviceName)
}

func configureCompactorGRPCServicePKI(sts *appsv1.StatefulSet, opts Options) error {
	serviceName := serviceNameCompactorGRPC(opts.Name)
	return configureGRPCServicePKI(&sts.Spec.Template.Spec, serviceName)
}

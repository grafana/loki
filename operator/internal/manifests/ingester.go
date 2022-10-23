package manifests

import (
	"fmt"
	"path"

	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	"github.com/grafana/loki/operator/internal/manifests/storage"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/imdario/mergo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildIngester builds the k8s objects required to run Loki Ingester
func BuildIngester(opts Options) ([]client.Object, error) {
	statefulSet := NewIngesterStatefulSet(opts)
	if opts.Gates.HTTPEncryption {
		if err := configureIngesterHTTPServicePKI(statefulSet, opts); err != nil {
			return nil, err
		}
	}

	if err := storage.ConfigureStatefulSet(statefulSet, opts.ObjectStorage); err != nil {
		return nil, err
	}

	if opts.Gates.GRPCEncryption {
		if err := configureIngesterGRPCServicePKI(statefulSet, opts); err != nil {
			return nil, err
		}
	}

	return []client.Object{
		statefulSet,
		NewIngesterGRPCService(opts),
		NewIngesterHTTPService(opts),
	}, nil
}

// NewIngesterStatefulSet creates a deployment object for an ingester
func NewIngesterStatefulSet(opts Options) *appsv1.StatefulSet {
	podSpec := corev1.PodSpec{
		Affinity: defaultAffinity(opts.Gates.DefaultNodeAffinity),
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
				Name:  "loki-ingester",
				Resources: corev1.ResourceRequirements{
					Limits:   opts.ResourceRequirements.Ingester.Limits,
					Requests: opts.ResourceRequirements.Ingester.Requests,
				},
				Args: []string{
					"-target=ingester",
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
					{
						Name:      walVolumeName,
						ReadOnly:  false,
						MountPath: walDirectory,
					},
				},
				TerminationMessagePath:   "/dev/termination-log",
				TerminationMessagePolicy: "File",
				ImagePullPolicy:          "IfNotPresent",
				SecurityContext:          containerSecurityContext(),
			},
		},
		SecurityContext: podSecurityContext(opts.Gates.RuntimeSeccompProfile),
	}

	if opts.Gates.HTTPEncryption || opts.Gates.GRPCEncryption {
		podSpec.Containers[0].Args = append(podSpec.Containers[0].Args,
			fmt.Sprintf("-server.tls-cipher-suites=%s", opts.TLSCipherSuites()),
			fmt.Sprintf("-server.tls-min-version=%s", opts.TLSProfile.MinTLSVersion),
		)
	}

	if opts.Stack.Template != nil && opts.Stack.Template.Ingester != nil {
		podSpec.Tolerations = opts.Stack.Template.Ingester.Tolerations
		podSpec.NodeSelector = opts.Stack.Template.Ingester.NodeSelector
	}

	l := ComponentLabels(LabelIngesterComponent, opts.Name)
	a := commonAnnotations(opts.ConfigSHA1)
	return &appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind:       "StatefulSet",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   IngesterName(opts.Name),
			Labels: l,
		},
		Spec: appsv1.StatefulSetSpec{
			PodManagementPolicy:  appsv1.OrderedReadyPodManagement,
			RevisionHistoryLimit: pointer.Int32Ptr(10),
			Replicas:             pointer.Int32Ptr(opts.Stack.Template.Ingester.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("loki-ingester-%s", opts.Name),
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
								corev1.ResourceStorage: opts.ResourceRequirements.Ingester.PVCSize,
							},
						},
						StorageClassName: pointer.StringPtr(opts.Stack.StorageClassName),
						VolumeMode:       &volumeFileSystemMode,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Labels: l,
						Name:   walVolumeName,
					},
					Spec: corev1.PersistentVolumeClaimSpec{
						AccessModes: []corev1.PersistentVolumeAccessMode{
							// TODO: should we verify that this is possible with the given storage class first?
							corev1.ReadWriteOnce,
						},
						Resources: corev1.ResourceRequirements{
							Requests: map[corev1.ResourceName]resource.Quantity{
								corev1.ResourceStorage: opts.ResourceRequirements.WALStorage.PVCSize,
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

// NewIngesterGRPCService creates a k8s service for the ingester GRPC endpoint
func NewIngesterGRPCService(opts Options) *corev1.Service {
	serviceName := serviceNameIngesterGRPC(opts.Name)
	labels := ComponentLabels(LabelIngesterComponent, opts.Name)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceNameIngesterGRPC(opts.Name),
			Labels:      labels,
			Annotations: serviceAnnotations(serviceName, opts.Gates.OpenShift.ServingCertsService),
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

// NewIngesterHTTPService creates a k8s service for the ingester HTTP endpoint
func NewIngesterHTTPService(opts Options) *corev1.Service {
	serviceName := serviceNameIngesterHTTP(opts.Name)
	labels := ComponentLabels(LabelIngesterComponent, opts.Name)

	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        serviceName,
			Labels:      labels,
			Annotations: serviceAnnotations(serviceName, opts.Gates.OpenShift.ServingCertsService),
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

func configureIngesterHTTPServicePKI(statefulSet *appsv1.StatefulSet, opts Options) error {
	serviceName := serviceNameIngesterHTTP(opts.Name)
	return configureHTTPServicePKI(&statefulSet.Spec.Template.Spec, serviceName)
}

func configureIngesterGRPCServicePKI(sts *appsv1.StatefulSet, opts Options) error {
	caBundleName := signingCABundleName(opts.Name)
	secretVolumeSpec := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: caBundleName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: caBundleName,
						},
					},
				},
			},
		},
	}

	secretContainerSpec := corev1.Container{
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      caBundleName,
				ReadOnly:  false,
				MountPath: caBundleDir,
			},
		},
		Args: []string{
			// Enable GRPC over TLS for ingester client
			"-ingester.client.tls-enabled=true",
			fmt.Sprintf("-ingester.client.tls-cipher-suites=%s", opts.TLSCipherSuites()),
			fmt.Sprintf("-ingester.client.tls-min-version=%s", opts.TLSProfile.MinTLSVersion),
			fmt.Sprintf("-ingester.client.tls-ca-path=%s", signingCAPath()),
			fmt.Sprintf("-ingester.client.tls-server-name=%s", fqdn(serviceNameIngesterGRPC(opts.Name), opts.Namespace)),
			// Enable GRPC over TLS for boltb-shipper index-gateway client
			"-boltdb.shipper.index-gateway-client.grpc.tls-enabled=true",
			fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-cipher-suites=%s", opts.TLSCipherSuites()),
			fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-min-version=%s", opts.TLSProfile.MinTLSVersion),
			fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-ca-path=%s", signingCAPath()),
			fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-server-name=%s", fqdn(serviceNameIndexGatewayGRPC(opts.Name), opts.Namespace)),
		},
	}

	if err := mergo.Merge(&sts.Spec.Template.Spec, secretVolumeSpec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge volumes")
	}

	if err := mergo.Merge(&sts.Spec.Template.Spec.Containers[0], secretContainerSpec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge container")
	}

	serviceName := serviceNameIngesterGRPC(opts.Name)
	return configureGRPCServicePKI(&sts.Spec.Template.Spec, serviceName)
}

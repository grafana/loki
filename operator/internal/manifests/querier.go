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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildQuerier returns a list of k8s objects for Loki Querier
func BuildQuerier(opts Options) ([]client.Object, error) {
	deployment := NewQuerierDeployment(opts)
	if opts.Gates.HTTPEncryption {
		if err := configureQuerierHTTPServicePKI(deployment, opts); err != nil {
			return nil, err
		}
	}

	if err := storage.ConfigureDeployment(deployment, opts.ObjectStorage); err != nil {
		return nil, err
	}

	if opts.Gates.GRPCEncryption {
		if err := configureQuerierGRPCServicePKI(deployment, opts); err != nil {
			return nil, err
		}
	}

	if opts.Gates.HTTPEncryption || opts.Gates.GRPCEncryption {
		caBundleName := signingCABundleName(opts.Name)
		if err := configureServiceCA(&deployment.Spec.Template.Spec, caBundleName); err != nil {
			return nil, err
		}
	}

	return []client.Object{
		deployment,
		NewQuerierGRPCService(opts),
		NewQuerierHTTPService(opts),
	}, nil
}

// NewQuerierDeployment creates a deployment object for a querier
func NewQuerierDeployment(opts Options) *appsv1.Deployment {
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
				Name:  "loki-querier",
				Resources: corev1.ResourceRequirements{
					Limits:   opts.ResourceRequirements.Querier.Limits,
					Requests: opts.ResourceRequirements.Querier.Requests,
				},
				Args: []string{
					"-target=querier",
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
				},
				TerminationMessagePath:   "/dev/termination-log",
				TerminationMessagePolicy: "File",
				ImagePullPolicy:          "IfNotPresent",
				SecurityContext:          containerSecurityContext(),
			},
		},
		SecurityContext: podSecurityContext(opts.Gates.RuntimeSeccompProfile),
	}

	podSpec = addProxyEnvVar(opts.Stack.Proxy, podSpec)

	if opts.Gates.HTTPEncryption || opts.Gates.GRPCEncryption {
		podSpec.Containers[0].Args = append(podSpec.Containers[0].Args,
			fmt.Sprintf("-server.tls-cipher-suites=%s", opts.TLSCipherSuites()),
			fmt.Sprintf("-server.tls-min-version=%s", opts.TLSProfile.MinTLSVersion),
		)
	}

	if opts.Stack.Template != nil && opts.Stack.Template.Querier != nil {
		podSpec.Tolerations = opts.Stack.Template.Querier.Tolerations
		podSpec.NodeSelector = opts.Stack.Template.Querier.NodeSelector
	}

	l := ComponentLabels(LabelQuerierComponent, opts.Name)
	a := commonAnnotations(opts.ConfigSHA1, opts.CertRotationRequiredAt)

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   QuerierName(opts.Name),
			Labels: l,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32Ptr(opts.Stack.Template.Querier.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("loki-querier-%s", opts.Name),
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

// NewQuerierGRPCService creates a k8s service for the querier GRPC endpoint
func NewQuerierGRPCService(opts Options) *corev1.Service {
	serviceName := serviceNameQuerierGRPC(opts.Name)
	labels := ComponentLabels(LabelQuerierComponent, opts.Name)

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

// NewQuerierHTTPService creates a k8s service for the querier HTTP endpoint
func NewQuerierHTTPService(opts Options) *corev1.Service {
	serviceName := serviceNameQuerierHTTP(opts.Name)
	labels := ComponentLabels(LabelQuerierComponent, opts.Name)

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

func configureQuerierHTTPServicePKI(deployment *appsv1.Deployment, opts Options) error {
	serviceName := serviceNameQuerierHTTP(opts.Name)
	return configureHTTPServicePKI(&deployment.Spec.Template.Spec, serviceName, opts.TLSProfile.MinTLSVersion, opts.TLSCipherSuites())
}

func configureQuerierGRPCServicePKI(deployment *appsv1.Deployment, opts Options) error {
	secretContainerSpec := corev1.Container{
		Args: []string{
			// Enable HTTP over TLS for compactor delete client
			"-boltdb.shipper.compactor.client.tls-enabled=true",
			fmt.Sprintf("-boltdb.shipper.compactor.client.tls-cipher-suites=%s", opts.TLSCipherSuites()),
			fmt.Sprintf("-boltdb.shipper.compactor.client.tls-min-version=%s", opts.TLSProfile.MinTLSVersion),
			fmt.Sprintf("-boltdb.shipper.compactor.client.tls-ca-path=%s", signingCAPath()),
			fmt.Sprintf("-boltdb.shipper.compactor.client.tls-cert-path=%s", lokiServerGRPCTLSCert()),
			fmt.Sprintf("-boltdb.shipper.compactor.client.tls-key-path=%s", lokiServerGRPCTLSKey()),
			fmt.Sprintf("-boltdb.shipper.compactor.client.tls-server-name=%s", fqdn(serviceNameCompactorHTTP(opts.Name), opts.Namespace)),
			// Enable GRPC over TLS for ingester client
			"-ingester.client.tls-enabled=true",
			fmt.Sprintf("-ingester.client.tls-cipher-suites=%s", opts.TLSCipherSuites()),
			fmt.Sprintf("-ingester.client.tls-min-version=%s", opts.TLSProfile.MinTLSVersion),
			fmt.Sprintf("-ingester.client.tls-ca-path=%s", signingCAPath()),
			fmt.Sprintf("-ingester.client.tls-cert-path=%s", lokiServerGRPCTLSCert()),
			fmt.Sprintf("-ingester.client.tls-key-path=%s", lokiServerGRPCTLSKey()),
			fmt.Sprintf("-ingester.client.tls-server-name=%s", fqdn(serviceNameIngesterGRPC(opts.Name), opts.Namespace)),
			// Enable GRPC over TLS for query frontend client
			"-querier.frontend-client.tls-enabled=true",
			fmt.Sprintf("-querier.frontend-client.tls-cipher-suites=%s", opts.TLSCipherSuites()),
			fmt.Sprintf("-querier.frontend-client.tls-min-version=%s", opts.TLSProfile.MinTLSVersion),
			fmt.Sprintf("-querier.frontend-client.tls-ca-path=%s", signingCAPath()),
			fmt.Sprintf("-querier.frontend-client.tls-cert-path=%s", lokiServerGRPCTLSCert()),
			fmt.Sprintf("-querier.frontend-client.tls-key-path=%s", lokiServerGRPCTLSKey()),
			fmt.Sprintf("-querier.frontend-client.tls-server-name=%s", fqdn(serviceNameQueryFrontendGRPC(opts.Name), opts.Namespace)),
			// Enable GRPC over TLS for boltb-shipper index-gateway client
			"-boltdb.shipper.index-gateway-client.grpc.tls-enabled=true",
			fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-cipher-suites=%s", opts.TLSCipherSuites()),
			fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-min-version=%s", opts.TLSProfile.MinTLSVersion),
			fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-ca-path=%s", signingCAPath()),
			fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-cert-path=%s", lokiServerGRPCTLSCert()),
			fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-key-path=%s", lokiServerGRPCTLSKey()),
			fmt.Sprintf("-boltdb.shipper.index-gateway-client.grpc.tls-server-name=%s", fqdn(serviceNameIndexGatewayGRPC(opts.Name), opts.Namespace)),
		},
	}

	if err := mergo.Merge(&deployment.Spec.Template.Spec.Containers[0], secretContainerSpec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge container")
	}

	serviceName := serviceNameQuerierGRPC(opts.Name)
	return configureGRPCServicePKI(&deployment.Spec.Template.Spec, serviceName)
}

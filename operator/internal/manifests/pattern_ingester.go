package manifests

import (
	"fmt"
	"path"

	"github.com/ViaQ/logerr/v2/log"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func BuildPatternIngester(opts Options) ([]client.Object, error) {
	deployment := NewPatternIngesterDeployment(opts)
	logger := log.NewLogger("pattern-ingester")
	logger.Info(deployment.Name, deployment.Namespace)
	if opts.Gates.HTTPEncryption {
		if err := configurePatternIngesterHTTPServicePKI(deployment, opts); err != nil {
			return nil, err
		}
	}

	if opts.Gates.GRPCEncryption {
		if err := configurePatternIngesterGRPCServicePKI(deployment, opts); err != nil {
			return nil, err
		}
	}

	if opts.Gates.HTTPEncryption || opts.Gates.GRPCEncryption {
		caBundleName := signingCABundleName(opts.Name)
		if err := configureServiceCA(&deployment.Spec.Template.Spec, caBundleName); err != nil {
			return nil, err
		}
	}

	if opts.Gates.RestrictedPodSecurityStandard {
		if err := configurePodSpecForRestrictedStandard(&deployment.Spec.Template.Spec); err != nil {
			return nil, err
		}
	}

	if err := configureHashRingEnv(&deployment.Spec.Template.Spec, opts); err != nil {
		return nil, err
	}

	if err := configureProxyEnv(&deployment.Spec.Template.Spec, opts); err != nil {
		return nil, err
	}

	if err := configureReplication(&deployment.Spec.Template, opts.Stack.Replication, LabelPatternIngesterComponent, opts.Name); err != nil {
		return nil, err
	}

	return []client.Object{
		deployment,
		NewPatternIngesterGRPCService(opts),
		NewPatternIngesterHTTPService(opts),
		newPatternIngesterPodDisruptionBudget(opts),
	}, nil
}

func NewPatternIngesterDeployment(opts Options) *appsv1.Deployment {
	logger := log.NewLogger("pattern-ingester")
	l := ComponentLabels(LabelPatternIngesterComponent, opts.Name)
	a := commonAnnotations(opts)
	podSpec := corev1.PodSpec{
		ServiceAccountName: opts.Name,
		Affinity: configureAffinity(LabelPatternIngesterComponent, opts.Name, opts.Gates.DefaultNodeAffinity, opts.Stack.Template.PatternIngester),
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
				Name: "loki-pattern-ingester",
				Resources: corev1.ResourceRequirements{
					Limits: opts.ResourceRequirements.PatternIngester.Limits,
					Requests: opts.ResourceRequirements.PatternIngester.Requests,
				},
				Args: []string{
					"-target=pattern-ingester",
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
			},
		},
	}

	if opts.Stack.Template != nil && opts.Stack.Template.PatternIngester != nil {
		podSpec.Tolerations = opts.Stack.Template.PatternIngester.Tolerations
		podSpec.NodeSelector = opts.Stack.Template.PatternIngester.NodeSelector
	}

	logger.Info("returning Pattern ingester deployment!!")

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   PatternIngesterName(opts.Name),
			Labels: l,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(opts.Stack.Template.PatternIngester.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("loki-pattern-ingester-%s", opts.Name),
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

// NewPatternIngesterGRPCService creates a k8s service for the PatternIngester GRPC endpoint
func NewPatternIngesterGRPCService(opts Options) *corev1.Service {
	serviceName := serviceNamePatternIngesterGRPC(opts.Name)
	labels := ComponentLabels(LabelPatternIngesterComponent, opts.Name)

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

// NewPatternIngesterHTTPService creates a k8s service for the PatternIngester HTTP endpoint
func NewPatternIngesterHTTPService(opts Options) *corev1.Service {
	serviceName := serviceNamePatternIngesterHTTP(opts.Name)
	labels := ComponentLabels(LabelPatternIngesterComponent, opts.Name)

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

func configurePatternIngesterHTTPServicePKI(deployment *appsv1.Deployment, opts Options) error {
	serviceName := serviceNameIngesterHTTP(opts.Name)
	return configureHTTPServicePKI(&deployment.Spec.Template.Spec, serviceName)
}

func configurePatternIngesterGRPCServicePKI(deployment *appsv1.Deployment, opts Options) error {
	serviceName := serviceNamePatternIngesterGRPC(opts.Name)
	return configureGRPCServicePKI(&deployment.Spec.Template.Spec, serviceName)
}


// newPatternIngesterPodDisruptionBudget returns a PodDisruptionBudget for the LokiStack
// PatternIngester pods.
func newPatternIngesterPodDisruptionBudget(opts Options) *policyv1.PodDisruptionBudget {
	l := ComponentLabels(LabelPatternIngesterComponent, opts.Name)
	mu := intstr.FromInt(1)
	return &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PodDisruptionBudget",
			APIVersion: policyv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels:    l,
			Name:      PatternIngesterName(opts.Name),
			Namespace: opts.Namespace,
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: l,
			},
			MinAvailable: &mu,
		},
	}
}
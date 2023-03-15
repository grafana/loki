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

// BuildQueryFrontend returns a list of k8s objects for Loki QueryFrontend
func BuildQueryFrontend(opts Options) ([]client.Object, error) {
	deployment := NewQueryFrontendDeployment(opts)
	if opts.Gates.HTTPEncryption {
		if err := configureQueryFrontendHTTPServicePKI(deployment, opts); err != nil {
			return nil, err
		}
	}

	if opts.Gates.GRPCEncryption {
		if err := configureQueryFrontendGRPCServicePKI(deployment, opts); err != nil {
			return nil, err
		}
	}

	if opts.Gates.HTTPEncryption || opts.Gates.GRPCEncryption {
		caBundleName := signingCABundleName(opts.Name)
		if err := configureServiceCA(&deployment.Spec.Template.Spec, caBundleName); err != nil {
			return nil, err
		}
	}

	if err := configureHashRingEnv(&deployment.Spec.Template.Spec, opts); err != nil {
		return nil, err
	}

	if err := configureProxyEnv(&deployment.Spec.Template.Spec, opts); err != nil {
		return nil, err
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
				Name:  lokiFrontendContainerName,
				Resources: corev1.ResourceRequirements{
					Limits:   opts.ResourceRequirements.QueryFrontend.Limits,
					Requests: opts.ResourceRequirements.QueryFrontend.Requests,
				},
				Args: []string{
					"-target=query-frontend",
					fmt.Sprintf("-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiConfigFileName)),
					fmt.Sprintf("-runtime-config.file=%s", path.Join(config.LokiConfigMountDir, config.LokiRuntimeConfigFileName)),
					"-config.expand-env=true",
				},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							// The frontend will only return ready once a querier has connected to it.
							// Because the service used for connecting the querier to the frontend only lists ready
							// instances there's sequencing issue. For now, we re-use the liveness-probe path
							// for the readiness-probe as a workaround.
							Path:   lokiLivenessPath,
							Port:   intstr.FromInt(httpPort),
							Scheme: corev1.URISchemeHTTP,
						},
					},
					PeriodSeconds:       10,
					InitialDelaySeconds: 15,
					TimeoutSeconds:      1,
					SuccessThreshold:    1,
					FailureThreshold:    3,
				},
				LivenessProbe: lokiLivenessProbe(),
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
				},
				TerminationMessagePath:   "/dev/termination-log",
				TerminationMessagePolicy: "File",
				ImagePullPolicy:          "IfNotPresent",
				SecurityContext:          containerSecurityContext(),
			},
		},
		SecurityContext: podSecurityContext(opts.Gates.RuntimeSeccompProfile),
	}

	if opts.Stack.Template != nil && opts.Stack.Template.QueryFrontend != nil {
		podSpec.Tolerations = opts.Stack.Template.QueryFrontend.Tolerations
		podSpec.NodeSelector = opts.Stack.Template.QueryFrontend.NodeSelector
	}

	l := ComponentLabels(LabelQueryFrontendComponent, opts.Name)
	a := commonAnnotations(opts.ConfigSHA1, opts.CertRotationRequiredAt)

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
			Replicas: pointer.Int32(opts.Stack.Template.QueryFrontend.Replicas),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        fmt.Sprintf("%s-%s", lokiFrontendContainerName, opts.Name),
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
	serviceName := serviceNameQueryFrontendGRPC(opts.Name)
	labels := ComponentLabels(LabelQueryFrontendComponent, opts.Name)

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

// NewQueryFrontendHTTPService creates a k8s service for the query-frontend HTTP endpoint
func NewQueryFrontendHTTPService(opts Options) *corev1.Service {
	serviceName := serviceNameQueryFrontendHTTP(opts.Name)
	labels := ComponentLabels(LabelQueryFrontendComponent, opts.Name)

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

func configureQueryFrontendHTTPServicePKI(deployment *appsv1.Deployment, opts Options) error {
	serviceName := serviceNameQueryFrontendHTTP(opts.Name)
	return configureHTTPServicePKI(&deployment.Spec.Template.Spec, serviceName)
}

func configureQueryFrontendGRPCServicePKI(deployment *appsv1.Deployment, opts Options) error {
	serviceName := serviceNameQueryFrontendGRPC(opts.Name)
	return configureGRPCServicePKI(&deployment.Spec.Template.Spec, serviceName)
}

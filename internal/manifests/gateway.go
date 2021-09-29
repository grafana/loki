package manifests

import (
	"crypto/sha1"
	"fmt"
	"path"

	"github.com/ViaQ/logerr/kverrors"
	"github.com/imdario/mergo"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/ViaQ/loki-operator/internal/manifests/internal/gateway"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

// BuildGateway returns a list of k8s objects for Loki Stack Gateway
func BuildGateway(opts Options) ([]client.Object, error) {
	gatewayCm, sha1C, err := gatewayConfigMap(opts)
	if err != nil {
		return nil, err
	}

	deployment := NewGatewayDeployment(opts, sha1C)
	if opts.Flags.EnableTLSServiceMonitorConfig {
		if err := configureGatewayMetricsPKI(&deployment.Spec.Template.Spec); err != nil {
			return nil, err
		}
	}

	return []client.Object{
		gatewayCm,
		deployment,
		NewGatewayHTTPService(opts),
	}, nil
}

// NewGatewayDeployment creates a deployment object for a lokiStack-gateway
func NewGatewayDeployment(opts Options, sha1C string) *appsv1.Deployment {
	podSpec := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: "rbac",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: LabelGatewayComponent,
						},
					},
				},
			},
			{
				Name: "tenants",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: LabelGatewayComponent,
						},
					},
				},
			},
			{
				Name: "lokistack-gateway",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: LabelGatewayComponent,
						},
					},
				},
			},
		},
		Containers: []corev1.Container{
			{
				Name:  LabelGatewayComponent,
				Image: DefaultLokiStackGatewayImage,
				Resources: corev1.ResourceRequirements{
					Limits:   opts.ResourceRequirements.Gateway.Limits,
					Requests: opts.ResourceRequirements.Gateway.Requests,
				},
				Args: []string{
					fmt.Sprintf("--debug.name=%s", LabelGatewayComponent),
					"--web.listen=0.0.0.0:8080",
					"--web.internal.listen=0.0.0.0:8081",
					"--log.level=debug",
					fmt.Sprintf("--logs.read.endpoint=http://%s:%d", fqdn(serviceNameQueryFrontendHTTP(opts.Name), opts.Namespace), httpPort),
					fmt.Sprintf("--logs.tail.endpoint=http://%s:%d", fqdn(serviceNameQueryFrontendHTTP(opts.Name), opts.Namespace), httpPort),
					fmt.Sprintf("--logs.write.endpoint=http://%s:%d", fqdn(serviceNameDistributorHTTP(opts.Name), opts.Namespace), httpPort),
					fmt.Sprintf("--rbac.config=%s", path.Join(gateway.LokiGatewayMountDir, gateway.LokiGatewayRbacFileName)),
					fmt.Sprintf("--tenants.config=%s", path.Join(gateway.LokiGatewayMountDir, gateway.LokiGatewayTenantFileName)),
				},
				Ports: []corev1.ContainerPort{
					{
						Name:          "internal",
						ContainerPort: 8081,
					},
					{
						Name:          "public",
						ContainerPort: 8080,
					},
					{
						Name:          "metrics",
						ContainerPort: httpPort,
					},
				},
				VolumeMounts: []corev1.VolumeMount{
					{
						Name:      "rbac",
						ReadOnly:  true,
						MountPath: path.Join(gateway.LokiGatewayMountDir, gateway.LokiGatewayRbacFileName),
						SubPath:   "rbac.yaml",
					},
					{
						Name:      "tenants",
						ReadOnly:  true,
						MountPath: path.Join(gateway.LokiGatewayMountDir, gateway.LokiGatewayTenantFileName),
						SubPath:   "tenants.yaml",
					},
					{
						Name:      "lokistack-gateway",
						ReadOnly:  true,
						MountPath: path.Join(gateway.LokiGatewayMountDir, gateway.LokiGatewayRegoFileName),
						SubPath:   "lokistack-gateway.rego",
					},
				},
				LivenessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "/live",
							Port:   intstr.FromInt(8081),
							Scheme: corev1.URISchemeHTTP,
						},
					},
					TimeoutSeconds:   2,
					PeriodSeconds:    30,
					FailureThreshold: 10,
				},
				ReadinessProbe: &corev1.Probe{
					Handler: corev1.Handler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "/ready",
							Port:   intstr.FromInt(8081),
							Scheme: corev1.URISchemeHTTP,
						},
					},
					TimeoutSeconds:   1,
					PeriodSeconds:    5,
					FailureThreshold: 12,
				},
			},
		},
	}

	l := ComponentLabels(LabelGatewayComponent, opts.Name)
	a := commonAnnotations(sha1C)

	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: appsv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   GatewayName(opts.Name),
			Labels: l,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: labels.Merge(l, GossipLabels()),
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        GatewayName(opts.Name),
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

// NewGatewayHTTPService creates a k8s service for the lokistack-gateway HTTP endpoint
func NewGatewayHTTPService(opts Options) *corev1.Service {
	serviceName := serviceNameGatewayHTTP(opts.Name)
	l := ComponentLabels(LabelGatewayComponent, opts.Name)
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
					Name: "metrics",
					Port: httpPort,
				},
			},
			Selector: l,
		},
	}
}

// gatewayConfigMap creates a configMap for rbac.yaml and tenants.yaml
func gatewayConfigMap(opt Options) (*corev1.ConfigMap, string, error) {
	cfg := gatewayConfigOptions(opt)
	rbacConfig, tenantsConfig, regoConfig, err := gateway.Build(cfg)
	if err != nil {
		return nil, "", err
	}

	s := sha1.New()
	_, err = s.Write(tenantsConfig)
	if err != nil {
		return nil, "", err
	}
	sha1C := fmt.Sprintf("%x", s.Sum(nil))

	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:   LabelGatewayComponent,
			Labels: commonLabels(opt.Name),
		},
		BinaryData: map[string][]byte{
			gateway.LokiGatewayRbacFileName:   rbacConfig,
			gateway.LokiGatewayTenantFileName: tenantsConfig,
			gateway.LokiGatewayRegoFileName:   regoConfig,
		},
	}, sha1C, nil
}

// gatewayConfigOptions converts Options to gateway.Options
func gatewayConfigOptions(opt Options) gateway.Options {
	var gatewaySecrets []*gateway.Secret
	for _, secret := range opt.TenantSecrets {
		gatewaySecret := &gateway.Secret{
			TenantName:   secret.TenantName,
			ClientID:     secret.ClientID,
			ClientSecret: secret.ClientSecret,
			IssuerCAPath: secret.IssuerCAPath,
		}
		gatewaySecrets = append(gatewaySecrets, gatewaySecret)
	}

	return gateway.Options{
		Stack:         opt.Stack,
		Namespace:     opt.Namespace,
		Name:          opt.Name,
		TenantSecrets: gatewaySecrets,
	}
}

func configureGatewayMetricsPKI(podSpec *corev1.PodSpec) error {
	secretVolumeSpec := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: "tls-secret",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: LabelGatewayComponent,
					},
				},
			},
			{
				Name: "tls-configmap",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: LabelGatewayComponent,
						},
					},
				},
			},
		},
	}
	secretContainerSpec := corev1.Container{
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "tls-secret",
				ReadOnly:  true,
				MountPath: path.Join(gateway.LokiGatewayTLSDir, "cert"),
				SubPath:   "cert",
			},
			{
				Name:      "tls-secret",
				ReadOnly:  true,
				MountPath: path.Join(gateway.LokiGatewayTLSDir, "key"),
				SubPath:   "key",
			},
			{
				Name:      "tls-configmap",
				ReadOnly:  true,
				MountPath: path.Join(gateway.LokiGatewayTLSDir, "ca"),
				SubPath:   "ca",
			},
		},
		Args: []string{
			fmt.Sprintf("--tls.internal.server.cert-file=%s", path.Join(gateway.LokiGatewayTLSDir, "cert")),
			fmt.Sprintf("--tls.internal.server.key-file=%s", path.Join(gateway.LokiGatewayTLSDir, "key")),
			fmt.Sprintf("--tls.healthchecks.server-ca-file=%s", path.Join(gateway.LokiGatewayTLSDir, "ca")),
		},
	}
	uriSchemeContainerSpec := corev1.Container{
		ReadinessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},
		LivenessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},
	}

	if err := mergo.Merge(podSpec, secretVolumeSpec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge volumes")
	}

	if err := mergo.Merge(&podSpec.Containers[0], secretContainerSpec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge container")
	}

	if err := mergo.Merge(&podSpec.Containers[0], uriSchemeContainerSpec, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge container")
	}

	return nil
}

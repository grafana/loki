package manifests

import (
	"crypto/sha1"
	"fmt"
	"path"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/imdario/mergo"

	"github.com/grafana/loki/operator/internal/manifests/internal/gateway"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	tlsMetricsSercetVolume = "tls-metrics-secret"
)

// BuildGateway returns a list of k8s objects for Loki Stack Gateway
func BuildGateway(opts Options) ([]client.Object, error) {
	cm, sha1C, err := gatewayConfigMap(opts)
	if err != nil {
		return nil, err
	}

	dpl := NewGatewayDeployment(opts, sha1C)
	svc := NewGatewayHTTPService(opts)

	ing, err := NewGatewayIngress(opts)
	if err != nil {
		return nil, err
	}

	objs := []client.Object{cm, dpl, svc, ing}

	if opts.Flags.EnableTLSServiceMonitorConfig {
		serviceName := serviceNameGatewayHTTP(opts.Name)
		if err := configureGatewayMetricsPKI(&dpl.Spec.Template.Spec, serviceName); err != nil {
			return nil, err
		}
	}

	if opts.Stack.Tenants != nil {
		mode := opts.Stack.Tenants.Mode
		if err := configureDeploymentForMode(dpl, mode, opts.Flags); err != nil {
			return nil, err
		}

		if err := configureServiceForMode(&svc.Spec, mode); err != nil {
			return nil, err
		}

		objs = configureGatewayObjsForMode(objs, opts)
	}

	return objs, nil
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
							Name: GatewayName(opts.Name),
						},
					},
				},
			},
			{
				Name: "tenants",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: GatewayName(opts.Name),
						},
					},
				},
			},
			{
				Name: "lokistack-gateway",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: GatewayName(opts.Name),
						},
					},
				},
			},
		},
		Containers: []corev1.Container{
			{
				Name:  gatewayContainerName,
				Image: opts.GatewayImage,
				Resources: corev1.ResourceRequirements{
					Limits:   opts.ResourceRequirements.Gateway.Limits,
					Requests: opts.ResourceRequirements.Gateway.Requests,
				},
				Args: []string{
					fmt.Sprintf("--debug.name=%s", LabelGatewayComponent),
					fmt.Sprintf("--web.listen=0.0.0.0:%d", gatewayHTTPPort),
					fmt.Sprintf("--web.internal.listen=0.0.0.0:%d", gatewayInternalPort),
					fmt.Sprintf("--web.healthchecks.url=http://localhost:%d", gatewayHTTPPort),
					"--log.level=warn",
					fmt.Sprintf("--logs.read.endpoint=http://%s:%d", fqdn(serviceNameQueryFrontendHTTP(opts.Name), opts.Namespace), httpPort),
					fmt.Sprintf("--logs.tail.endpoint=http://%s:%d", fqdn(serviceNameQueryFrontendHTTP(opts.Name), opts.Namespace), httpPort),
					fmt.Sprintf("--logs.write.endpoint=http://%s:%d", fqdn(serviceNameDistributorHTTP(opts.Name), opts.Namespace), httpPort),
					fmt.Sprintf("--rbac.config=%s", path.Join(gateway.LokiGatewayMountDir, gateway.LokiGatewayRbacFileName)),
					fmt.Sprintf("--tenants.config=%s", path.Join(gateway.LokiGatewayMountDir, gateway.LokiGatewayTenantFileName)),
				},
				Ports: []corev1.ContainerPort{
					{
						Name:          gatewayInternalPortName,
						ContainerPort: gatewayInternalPort,
					},
					{
						Name:          gatewayHTTPPortName,
						ContainerPort: gatewayHTTPPort,
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
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "/live",
							Port:   intstr.FromInt(gatewayInternalPort),
							Scheme: corev1.URISchemeHTTP,
						},
					},
					TimeoutSeconds:   2,
					PeriodSeconds:    30,
					FailureThreshold: 10,
				},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler: corev1.ProbeHandler{
						HTTPGet: &corev1.HTTPGetAction{
							Path:   "/ready",
							Port:   intstr.FromInt(gatewayInternalPort),
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
				MatchLabels: l,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Name:        GatewayName(opts.Name),
					Labels:      l,
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
					Name: gatewayHTTPPortName,
					Port: gatewayHTTPPort,
				},
				{
					Name: gatewayInternalPortName,
					Port: gatewayInternalPort,
				},
			},
			Selector: l,
		},
	}
}

// NewGatewayIngress creates a k8s Ingress object for accessing
// the lokistack-gateway from public.
func NewGatewayIngress(opts Options) (*networkingv1.Ingress, error) {
	pt := networkingv1.PathTypePrefix
	l := ComponentLabels(LabelGatewayComponent, opts.Name)

	ingBackend := networkingv1.IngressBackend{
		Service: &networkingv1.IngressServiceBackend{
			Name: serviceNameGatewayHTTP(opts.Name),
			Port: networkingv1.ServiceBackendPort{
				Name: gatewayHTTPPortName,
			},
		},
	}

	return &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Ingress",
			APIVersion: networkingv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels:    l,
			Name:      opts.Name,
			Namespace: opts.Namespace,
		},
		Spec: networkingv1.IngressSpec{
			DefaultBackend: &ingBackend,
			Rules: []networkingv1.IngressRule{
				{
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									Path:     "/api/logs/v1",
									PathType: &pt,
									Backend:  ingBackend,
								},
							},
						},
					},
				},
			},
		},
	}, nil
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
			Name:   GatewayName(opt.Name),
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

	tenantConfigMap := make(map[string]gateway.TenantData)
	for tenant, tenantData := range opt.TenantConfigMap {
		tenantConfigMap[tenant] = gateway.TenantData{
			CookieSecret: tenantData.CookieSecret,
		}
	}

	return gateway.Options{
		Stack:            opt.Stack,
		Namespace:        opt.Namespace,
		Name:             opt.Name,
		OpenShiftOptions: opt.OpenShiftOptions,
		TenantSecrets:    gatewaySecrets,
		TenantConfigMap:  tenantConfigMap,
	}
}

func configureGatewayMetricsPKI(podSpec *corev1.PodSpec, serviceName string) error {
	var gwIndex int
	for i, c := range podSpec.Containers {
		if c.Name == gatewayContainerName {
			gwIndex = i
			break
		}
	}

	secretName := signingServiceSecretName(serviceName)
	certFile := path.Join(gateway.LokiGatewayTLSDir, gateway.LokiGatewayCertFile)
	keyFile := path.Join(gateway.LokiGatewayTLSDir, gateway.LokiGatewayKeyFile)

	secretVolumeSpec := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: tlsMetricsSercetVolume,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: secretName,
					},
				},
			},
		},
	}
	secretContainerSpec := corev1.Container{
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      tlsMetricsSercetVolume,
				ReadOnly:  true,
				MountPath: gateway.LokiGatewayTLSDir,
			},
		},
		Args: []string{
			fmt.Sprintf("--tls.internal.server.cert-file=%s", certFile),
			fmt.Sprintf("--tls.internal.server.key-file=%s", keyFile),
		},
	}
	uriSchemeContainerSpec := corev1.Container{
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Scheme: corev1.URISchemeHTTPS,
				},
			},
		},
	}

	if err := mergo.Merge(podSpec, secretVolumeSpec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge volumes")
	}

	if err := mergo.Merge(&podSpec.Containers[gwIndex], secretContainerSpec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge container")
	}

	if err := mergo.Merge(&podSpec.Containers[gwIndex], uriSchemeContainerSpec, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge container")
	}

	return nil
}

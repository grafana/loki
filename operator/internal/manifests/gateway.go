package manifests

import (
	"crypto/sha1"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/imdario/mergo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/grafana/loki/operator/internal/manifests/internal/gateway"
)

const (
	tlsSecretVolume = "tls-secret"
)

var (
	logsEndpointRe     = regexp.MustCompile(`^--logs\.(?:read|tail|write|rules)\.endpoint=http://.+`)
	defaultAdminGroups = []string{
		"system:cluster-admins",
		"cluster-admin",
		"dedicated-admin",
	}
)

// BuildGateway returns a list of k8s objects for Loki Stack Gateway
func BuildGateway(opts Options) ([]client.Object, error) {
	cm, tenantSecret, sha1C, err := gatewayConfigObjs(opts)
	if err != nil {
		return nil, err
	}

	dpl := NewGatewayDeployment(opts, sha1C)
	sa := NewServiceAccount(opts)
	saToken := NewServiceAccountTokenSecret(opts)
	svc := NewGatewayHTTPService(opts)
	pdb := NewGatewayPodDisruptionBudget(opts)

	ing, err := NewGatewayIngress(opts)
	if err != nil {
		return nil, err
	}

	objs := []client.Object{cm, tenantSecret, dpl, sa, saToken, svc, ing, pdb}

	minTLSVersion := opts.TLSProfile.MinTLSVersion
	ciphersList := opts.TLSProfile.Ciphers
	ciphers := strings.Join(ciphersList, `,`)

	if opts.Stack.Rules != nil && opts.Stack.Rules.Enabled {
		if err := configureGatewayRulesAPI(&dpl.Spec.Template.Spec, opts.Name, opts.Namespace); err != nil {
			return nil, err
		}

		if opts.Stack.Tenants != nil {
			mode := opts.Stack.Tenants.Mode
			if err := configureGatewayDeploymentRulesAPIForMode(dpl, mode); err != nil {
				return nil, err
			}
		}
	}

	if opts.Gates.HTTPEncryption {
		serviceName := serviceNameGatewayHTTP(opts.Name)
		serverCAName := gatewaySigningCABundleName(GatewayName(opts.Name))
		upstreamCAName := signingCABundleName(opts.Name)
		upstreamClientName := gatewayClientSecretName(opts.Name)
		if err := configureGatewayServerPKI(&dpl.Spec.Template.Spec, opts.Namespace, serviceName, serverCAName, upstreamCAName, upstreamClientName, minTLSVersion, ciphers); err != nil {
			return nil, err
		}
	}

	if opts.Stack.Tenants != nil {
		adminGroups := defaultAdminGroups
		if opts.Stack.Tenants.Openshift != nil && opts.Stack.Tenants.Openshift.AdminGroups != nil {
			adminGroups = opts.Stack.Tenants.Openshift.AdminGroups
		}

		if err := configureGatewayDeploymentForMode(dpl, opts.Stack.Tenants, opts.Gates, minTLSVersion, ciphers, adminGroups); err != nil {
			return nil, err
		}

		if err := configureGatewayServiceForMode(&svc.Spec, opts.Stack.Tenants.Mode); err != nil {
			return nil, err
		}

		objs = configureGatewayObjsForMode(objs, opts)
	}

	if opts.Gates.RestrictedPodSecurityStandard {
		if err := configurePodSpecForRestrictedStandard(&dpl.Spec.Template.Spec); err != nil {
			return nil, err
		}
	}

	if err := configureReplication(&dpl.Spec.Template, opts.Stack.Replication, LabelGatewayComponent, opts.Name); err != nil {
		return nil, err
	}

	return objs, nil
}

// NewGatewayDeployment creates a deployment object for a lokiStack-gateway
func NewGatewayDeployment(opts Options, sha1C string) *appsv1.Deployment {
	l := ComponentLabels(LabelGatewayComponent, opts.Name)
	a := gatewayAnnotations(sha1C, opts.CertRotationRequiredAt)
	podSpec := corev1.PodSpec{
		ServiceAccountName: GatewayName(opts.Name),
		Affinity:           configureAffinity(LabelGatewayComponent, opts.Name, opts.Gates.DefaultNodeAffinity, opts.Stack.Template.Gateway),
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
					Secret: &corev1.SecretVolumeSource{
						SecretName: GatewayName(opts.Name),
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
					fmt.Sprintf("--logs.write-timeout=%s", opts.Timeouts.Gateway.UpstreamWriteTimeout),
					fmt.Sprintf("--rbac.config=%s", path.Join(gateway.LokiGatewayMountDir, gateway.LokiGatewayRbacFileName)),
					fmt.Sprintf("--tenants.config=%s", path.Join(gateway.LokiGatewayMountDir, gateway.LokiGatewayTenantFileName)),
					fmt.Sprintf("--server.read-timeout=%s", opts.Timeouts.Gateway.ReadTimeout),
					fmt.Sprintf("--server.write-timeout=%s", opts.Timeouts.Gateway.WriteTimeout),
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

	if opts.Stack.Template != nil && opts.Stack.Template.Gateway != nil {
		podSpec.Tolerations = opts.Stack.Template.Gateway.Tolerations
		podSpec.NodeSelector = opts.Stack.Template.Gateway.NodeSelector
	}

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
			Replicas: ptr.To(opts.Stack.Template.Gateway.Replicas),
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
	labels := ComponentLabels(LabelGatewayComponent, opts.Name)

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
					Name: gatewayHTTPPortName,
					Port: gatewayHTTPPort,
				},
				{
					Name: gatewayInternalPortName,
					Port: gatewayInternalPort,
				},
			},
			Selector: labels,
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

// NewServiceAccount returns a k8s object for the LokiStack Gateway
// serviceaccount.
func NewServiceAccount(opts Options) client.Object {
	l := ComponentLabels(LabelGatewayComponent, opts.Name)
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels:    l,
			Name:      GatewayName(opts.Name),
			Namespace: opts.Namespace,
		},
		AutomountServiceAccountToken: ptr.To(true),
	}
}

// NewServiceAccountTokenSecret returns a k8s object for the LokiStack
// Gateway secret. This secret represents the ServiceAccountToken.
func NewServiceAccountTokenSecret(opts Options) client.Object {
	l := ComponentLabels(LabelGatewayComponent, opts.Name)

	return &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				corev1.ServiceAccountNameKey: GatewayName(opts.Name),
			},
			Labels:    l,
			Name:      gatewayTokenSecretName(GatewayName(opts.Name)),
			Namespace: opts.Namespace,
		},
		Type: corev1.SecretTypeServiceAccountToken,
	}
}

// NewGatewayPodDisruptionBudget returns a PodDisruptionBudget for the LokiStack
// Gateway pods.
func NewGatewayPodDisruptionBudget(opts Options) *policyv1.PodDisruptionBudget {
	l := ComponentLabels(LabelGatewayComponent, opts.Name)
	mu := intstr.FromInt(1)
	return &policyv1.PodDisruptionBudget{
		TypeMeta: metav1.TypeMeta{
			Kind:       "PodDisruptionBudget",
			APIVersion: policyv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels:    l,
			Name:      GatewayName(opts.Name),
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

// gatewayConfigObjs creates a configMap for rbac.yaml and a secret for tenants.yaml
func gatewayConfigObjs(opt Options) (*corev1.ConfigMap, *corev1.Secret, string, error) {
	cfg := gatewayConfigOptions(opt)
	rbacConfig, tenantsConfig, regoConfig, err := gateway.Build(cfg)
	if err != nil {
		return nil, nil, "", err
	}

	s := sha1.New()
	_, err = s.Write(tenantsConfig)
	if err != nil {
		return nil, nil, "", err
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
				gateway.LokiGatewayRbacFileName: rbacConfig,
				gateway.LokiGatewayRegoFileName: regoConfig,
			},
		}, &corev1.Secret{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Secret",
				APIVersion: corev1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      GatewayName(opt.Name),
				Labels:    ComponentLabels(LabelGatewayComponent, opt.Name),
				Namespace: opt.Namespace,
			},
			Data: map[string][]byte{
				gateway.LokiGatewayTenantFileName: tenantsConfig,
			},
			Type: corev1.SecretTypeOpaque,
		}, sha1C, nil
}

// gatewayConfigOptions converts Options to gateway.Options
func gatewayConfigOptions(opt Options) gateway.Options {
	var (
		gatewaySecrets []*gateway.Secret
		gatewaySecret  *gateway.Secret
	)
	for _, secret := range opt.Tenants.Secrets {
		gatewaySecret = &gateway.Secret{
			TenantName: secret.TenantName,
		}
		switch {
		case secret.OIDCSecret != nil:
			gatewaySecret.OIDC = &gateway.OIDC{
				ClientID:     secret.OIDCSecret.ClientID,
				ClientSecret: secret.OIDCSecret.ClientSecret,
				IssuerCAPath: secret.OIDCSecret.IssuerCAPath,
			}
		case secret.MTLSSecret != nil:
			gatewaySecret.MTLS = &gateway.MTLS{
				CAPath: secret.MTLSSecret.CAPath,
			}
		}
		gatewaySecrets = append(gatewaySecrets, gatewaySecret)
	}

	return gateway.Options{
		Stack:            opt.Stack,
		Namespace:        opt.Namespace,
		Name:             opt.Name,
		OpenShiftOptions: opt.OpenShiftOptions,
		TenantSecrets:    gatewaySecrets,
	}
}

func configureGatewayServerPKI(
	podSpec *corev1.PodSpec,
	namespace, serviceName, serverCAName string,
	upstreamCAName, upstreamClientName string,
	minTLSVersion, ciphers string,
) error {
	var gwIndex int
	for i, c := range podSpec.Containers {
		if c.Name == gatewayContainerName {
			gwIndex = i
			break
		}
	}

	gwContainer := podSpec.Containers[gwIndex].DeepCopy()
	gwArgs := gwContainer.Args
	gwVolumes := podSpec.Volumes

	for i, a := range gwArgs {
		if strings.HasPrefix(a, "--web.healthchecks.url=") {
			gwArgs[i] = fmt.Sprintf("--web.healthchecks.url=https://localhost:%d", gatewayHTTPPort)
		}

		if logsEndpointRe.MatchString(a) {
			gwArgs[i] = strings.Replace(a, "http", "https", 1)
		}
	}

	serverName := fqdn(serviceName, namespace)
	gwArgs = append(gwArgs,
		"--tls.client-auth-type=NoClientCert",
		"--tls.min-version=VersionTLS12",
		fmt.Sprintf("--tls.server.cert-file=%s", gatewayServerHTTPTLSCert()),
		fmt.Sprintf("--tls.server.key-file=%s", gatewayServerHTTPTLSKey()),
		fmt.Sprintf("--tls.healthchecks.server-ca-file=%s", gatewaySigningCAPath()),
		fmt.Sprintf("--tls.healthchecks.server-name=%s", serverName),
		fmt.Sprintf("--tls.internal.server.cert-file=%s", gatewayServerHTTPTLSCert()),
		fmt.Sprintf("--tls.internal.server.key-file=%s", gatewayServerHTTPTLSKey()),
		fmt.Sprintf("--tls.min-version=%s", minTLSVersion),
		fmt.Sprintf("--tls.cipher-suites=%s", ciphers),
		fmt.Sprintf("--logs.tls.ca-file=%s", gatewayUpstreamCAPath()),
		fmt.Sprintf("--logs.tls.cert-file=%s", gatewayUpstreamHTTPTLSCert()),
		fmt.Sprintf("--logs.tls.key-file=%s", gatewayUpstreamHTTPTLSKey()),
	)

	gwContainer.ReadinessProbe.ProbeHandler.HTTPGet.Scheme = corev1.URISchemeHTTPS
	gwContainer.LivenessProbe.ProbeHandler.HTTPGet.Scheme = corev1.URISchemeHTTPS
	gwContainer.Args = gwArgs

	gwVolumes = append(gwVolumes,
		corev1.Volume{
			Name: tlsSecretVolume,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: serviceName,
				},
			},
		},
		corev1.Volume{
			Name: upstreamClientName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: upstreamClientName,
				},
			},
		},
		corev1.Volume{
			Name: upstreamCAName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					DefaultMode: &defaultConfigMapMode,
					LocalObjectReference: corev1.LocalObjectReference{
						Name: upstreamCAName,
					},
				},
			},
		},
		corev1.Volume{
			Name: serverCAName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					DefaultMode: &defaultConfigMapMode,
					LocalObjectReference: corev1.LocalObjectReference{
						Name: serverCAName,
					},
				},
			},
		},
	)

	gwContainer.VolumeMounts = append(
		gwContainer.VolumeMounts,
		corev1.VolumeMount{
			Name:      tlsSecretVolume,
			ReadOnly:  true,
			MountPath: gatewayServerHTTPTLSDir(),
		},
		corev1.VolumeMount{
			Name:      upstreamClientName,
			ReadOnly:  true,
			MountPath: gatewayUpstreamHTTPTLSDir(),
		},
		corev1.VolumeMount{
			Name:      upstreamCAName,
			ReadOnly:  true,
			MountPath: gatewayUpstreamCADir(),
		},
		corev1.VolumeMount{
			Name:      serverCAName,
			ReadOnly:  true,
			MountPath: gatewaySigningCADir(),
		},
	)

	p := corev1.PodSpec{
		Containers: []corev1.Container{
			*gwContainer,
		},
		Volumes: gwVolumes,
	}

	if err := mergo.Merge(podSpec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge server pki into container spec ")
	}

	return nil
}

func configureGatewayRulesAPI(podSpec *corev1.PodSpec, stackName, stackNs string) error {
	var gwIndex int
	for i, c := range podSpec.Containers {
		if c.Name == gatewayContainerName {
			gwIndex = i
			break
		}
	}

	container := corev1.Container{
		Args: []string{
			fmt.Sprintf("--logs.rules.endpoint=http://%s:%d", fqdn(serviceNameRulerHTTP(stackName), stackNs), httpPort),
			"--logs.rules.read-only=true",
		},
	}

	if err := mergo.Merge(&podSpec.Containers[gwIndex], container, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge container")
	}

	return nil
}

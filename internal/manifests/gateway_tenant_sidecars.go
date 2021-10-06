package manifests

import (
	"fmt"
	"os"
	"path"

	"github.com/ViaQ/logerr/kverrors"
	"github.com/google/uuid"
	"github.com/imdario/mergo"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/manifests/internal/gateway"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	envRelatedImageOPA = "RELATED_IMAGE_OPA"

	defaultOPAImage = "quay.io/observatorium/opa-openshift:latest"

	opaContainerName   = "opa-openshift"
	opaDefaultPackage  = "lokistack"
	opaDefaultAPIGroup = "loki.openshift.io"
	opaMetricsPortName = "opa-metrics"

	// OpenShiftApplicationTenant is the tenant name for tenant holding application logs.
	OpenShiftApplicationTenant = "application"
	// OpenShiftInfraTenant is the tenant name for tenant holding infrastructure logs.
	OpenShiftInfraTenant = "infrastructure"
	// OpenShiftAuditTenant is the tenant name for tenant holding audit logs.
	OpenShiftAuditTenant = "audit"
)

// OpenShiftDefaultTenants represents the slice of all supported LokiStack on OpenShift.
var OpenShiftDefaultTenants = []string{
	OpenShiftApplicationTenant,
	OpenShiftInfraTenant,
	OpenShiftAuditTenant,
}

// ApplyGatewayDefaultOptions applies defaults on the LokiStackSpec depending on selected
// tenant mode. Currently nothing is applied for modes static and dynamic. For mode openshift-logging
// the tenant spec is filled with defaults for authentication and authorization.
func ApplyGatewayDefaultOptions(opts *Options) error {
	switch opts.Stack.Tenants.Mode {
	case lokiv1beta1.Static, lokiv1beta1.Dynamic:
		return nil // continue using user input

	case lokiv1beta1.OpenshiftLogging:
		var authn []lokiv1beta1.AuthenticationSpec
		for _, name := range OpenShiftDefaultTenants {
			authn = append(authn, lokiv1beta1.AuthenticationSpec{
				TenantName: name,
				TenantID:   uuid.New().String(),
				OIDC: &lokiv1beta1.OIDCSpec{
					// TODO Setup when we integrate dex as a separate sidecar here
					IssuerURL:     "https://127.0.0.1:5556/dex",
					RedirectURL:   fmt.Sprintf("http://localhost:%d/oidc/%s/callback", gatewayHTTPPort, name),
					UsernameClaim: "name",
				},
			})
		}

		defaults := &lokiv1beta1.TenantsSpec{
			Authentication: authn,
			Authorization: &lokiv1beta1.AuthorizationSpec{
				OPA: &lokiv1beta1.OPASpec{
					URL: fmt.Sprintf("http://localhost:%d/data/%s/allow", gatewayOPAHTTPPort, opaDefaultPackage),
				},
			},
		}

		if err := mergo.Merge(opts.Stack.Tenants, defaults, mergo.WithOverride); err != nil {
			return kverrors.Wrap(err, "failed to merge defaults for mode openshift logging")
		}
	}

	return nil
}

func configureDeploymentForMode(d *appsv1.DeploymentSpec, mode lokiv1beta1.ModeType, flags FeatureFlags) error {
	switch mode {
	case lokiv1beta1.Static, lokiv1beta1.Dynamic:
		return nil // nothing to configure
	case lokiv1beta1.OpenshiftLogging:
		return configureDeploymentForOpenShiftLogging(d, flags)
	}

	return nil
}

func configureServiceForMode(s *corev1.ServiceSpec, mode lokiv1beta1.ModeType) error {
	switch mode {
	case lokiv1beta1.Static, lokiv1beta1.Dynamic:
		return nil // nothing to configure
	case lokiv1beta1.OpenshiftLogging:
		return configureServiceForOpenShiftLogging(s)
	}

	return nil
}

func configureServiceMonitorForMode(sm *monitoringv1.ServiceMonitor, mode lokiv1beta1.ModeType, flags FeatureFlags) error {
	switch mode {
	case lokiv1beta1.Static, lokiv1beta1.Dynamic:
		return nil // nothing to configure
	case lokiv1beta1.OpenshiftLogging:
		return configureServiceMonitorForOpenShiftLogging(sm, flags)
	}

	return nil
}

func configureDeploymentForOpenShiftLogging(d *appsv1.DeploymentSpec, flags FeatureFlags) error {
	p := corev1.PodSpec{
		Containers: []corev1.Container{
			newOPAOpenShiftContainer(flags),
		},
	}

	if err := mergo.Merge(&d.Template.Spec, p, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge sidecar containers")
	}

	return nil
}

func newOPAOpenShiftContainer(flags FeatureFlags) corev1.Container {
	var (
		image        string
		args         []string
		uriScheme    corev1.URIScheme
		volumeMounts []corev1.VolumeMount
	)

	image = os.Getenv(envRelatedImageOPA)
	if image == "" {
		image = defaultOPAImage
	}

	uriScheme = corev1.URISchemeHTTP
	args = []string{
		"--log.level=warn",
		fmt.Sprintf("--opa.package=%s", opaDefaultPackage),
		fmt.Sprintf("--web.listen=:%d", gatewayOPAHTTPPort),
		fmt.Sprintf("--web.internal.listen=:%d", gatewayOPAInternalPort),
		fmt.Sprintf("--web.healthchecks.url=http://localhost:%d", gatewayOPAHTTPPort),
	}

	if flags.EnableTLSServiceMonitorConfig {
		certFile := path.Join(gateway.LokiGatewayTLSDir, gateway.LokiGatewayCertFile)
		keyFile := path.Join(gateway.LokiGatewayTLSDir, gateway.LokiGatewayKeyFile)

		args = append(args, []string{
			fmt.Sprintf("--tls.internal.server.cert-file=%s", certFile),
			fmt.Sprintf("--tls.internal.server.key-file=%s", keyFile),
		}...)

		uriScheme = corev1.URISchemeHTTPS

		volumeMounts = []corev1.VolumeMount{
			{
				Name:      tlsMetricsSercetVolume,
				ReadOnly:  true,
				MountPath: gateway.LokiGatewayTLSDir,
			},
		}
	}

	for _, t := range OpenShiftDefaultTenants {
		args = append(args, fmt.Sprintf(`--openshift.mappings=%s=%s`, t, opaDefaultAPIGroup))
	}

	return corev1.Container{
		Name:  opaContainerName,
		Image: image,
		Args:  args,
		Ports: []corev1.ContainerPort{
			{
				Name:          gatewayOPAHTTPPortName,
				ContainerPort: gatewayOPAHTTPPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          gatewayOPAInternalPortName,
				ContainerPort: gatewayOPAInternalPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		LivenessProbe: &corev1.Probe{
			Handler: corev1.Handler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/live",
					Port:   intstr.FromInt(gatewayOPAInternalPort),
					Scheme: uriScheme,
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
					Port:   intstr.FromInt(gatewayOPAInternalPort),
					Scheme: uriScheme,
				},
			},
			TimeoutSeconds:   1,
			PeriodSeconds:    5,
			FailureThreshold: 12,
		},
		VolumeMounts: volumeMounts,
	}
}

func configureServiceForOpenShiftLogging(s *corev1.ServiceSpec) error {
	spec := corev1.ServiceSpec{
		Ports: []corev1.ServicePort{
			{
				Name: opaMetricsPortName,
				Port: gatewayOPAInternalPort,
			},
		},
	}

	if err := mergo.Merge(s, spec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge sidecar containers")
	}

	return nil
}

func configureServiceMonitorForOpenShiftLogging(sm *monitoringv1.ServiceMonitor, flags FeatureFlags) error {
	var opaEndpoint monitoringv1.Endpoint

	if flags.EnableTLSServiceMonitorConfig {
		tlsConfig := sm.Spec.Endpoints[0].TLSConfig
		opaEndpoint = monitoringv1.Endpoint{
			Port:            opaMetricsPortName,
			Path:            "/metrics",
			Scheme:          "https",
			BearerTokenFile: BearerTokenFile,
			TLSConfig:       tlsConfig,
		}
	} else {
		opaEndpoint = monitoringv1.Endpoint{
			Port:   opaMetricsPortName,
			Path:   "/metrics",
			Scheme: "http",
		}
	}

	spec := monitoringv1.ServiceMonitorSpec{
		Endpoints: []monitoringv1.Endpoint{opaEndpoint},
	}

	if err := mergo.Merge(&sm.Spec, spec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge sidecar service monitor endpoints")
	}

	return nil
}

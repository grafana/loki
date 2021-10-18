package openshift

import (
	"github.com/ViaQ/logerr/kverrors"
	"github.com/imdario/mergo"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
)

const (
	// tenantApplication is the name of the tenant holding application logs.
	tenantApplication = "application"
	// tenantInfrastructure is the name of the tenant holding infrastructure logs.
	tenantInfrastructure = "infrastructure"
	// tenantAudit is the name of the tenant holding audit logs.
	tenantAudit = "audit"
)

// defaultTenants represents the slice of all supported LokiStack on OpenShift.
var defaultTenants = []string{
	tenantApplication,
	tenantInfrastructure,
	tenantAudit,
}

// ConfigureDeployment merges an OpenPolicyAgent sidecar into the deployment spec.
// With this, the deployment will route authorization request to the OpenShift
// apiserver through the sidecar.
func ConfigureDeployment(d *appsv1.DeploymentSpec, sercretVolumeName, tlsDir, certFile, keyFile string, withTLS bool) error {
	p := corev1.PodSpec{
		Containers: []corev1.Container{
			newOPAOpenShiftContainer(sercretVolumeName, tlsDir, certFile, keyFile, withTLS),
		},
	}

	if err := mergo.Merge(&d.Template.Spec, p, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge sidecar container spec ")
	}

	return nil
}

// ConfigureService merges the OpenPolicyAgent sidecar metrics port into
// the service spec. With this the metrics are exposed through the same service.
func ConfigureService(s *corev1.ServiceSpec) error {
	spec := corev1.ServiceSpec{
		Ports: []corev1.ServicePort{
			{
				Name: opaMetricsPortName,
				Port: GatewayOPAInternalPort,
			},
		},
	}

	if err := mergo.Merge(s, spec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge sidecar service ports")
	}

	return nil
}

// ConfigureIngress merges the OpenShift Route-specific annotations to
// the lokistack gateway ingress object.
func ConfigureIngress(i *networkingv1.Ingress) error {
	ing := networkingv1.Ingress{}

	if err := mergo.Merge(i, ing); err != nil {
		return kverrors.Wrap(err, "failed to merge ingress config")
	}

	return nil
}

// ConfigureServiceMonitor merges the OpenPolicyAgent sidecar endpoint into
// the service monitor. With this cluster-monitoring prometheus can scrape
// the sidecar metrics.
func ConfigureServiceMonitor(sm *monitoringv1.ServiceMonitor, withTLS bool) error {
	var opaEndpoint monitoringv1.Endpoint

	if withTLS {
		tlsConfig := sm.Spec.Endpoints[0].TLSConfig
		opaEndpoint = monitoringv1.Endpoint{
			Port:            opaMetricsPortName,
			Path:            "/metrics",
			Scheme:          "https",
			BearerTokenFile: bearerTokenFile,
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

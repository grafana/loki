package openshift

import (
	"fmt"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/imdario/mergo"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
)

const (
	// tenantApplication is the name of the tenant holding application logs.
	tenantApplication = "application"
	// tenantInfrastructure is the name of the tenant holding infrastructure logs.
	tenantInfrastructure = "infrastructure"
	// tenantAudit is the name of the tenant holding audit logs.
	tenantAudit = "audit"
	// tenantNetwork is the name of the tenant holding network logs.
	tenantNetwork = "network"
)

var (
	// loggingTenants represents the slice of all supported tenants on OpenshiftLogging mode.
	loggingTenants = []string{
		tenantApplication,
		tenantInfrastructure,
		tenantAudit,
	}

	// networkTenants represents the slice of all supported tenants on OpenshiftNetwork mode.
	networkTenants = []string{
		tenantNetwork,
	}
)

// GetTenants return the slice of all supported tenants for a specified mode
func GetTenants(mode lokiv1.ModeType) []string {
	switch mode {
	case lokiv1.OpenshiftLogging:
		return loggingTenants
	case lokiv1.OpenshiftNetwork:
		return networkTenants
	default:
		return []string{}
	}
}

// ConfigureGatewayDeployment merges an OpenPolicyAgent sidecar into the deployment spec.
// With this, the deployment will route authorization request to the OpenShift
// apiserver through the sidecar.
// This function also forces the use of a TLS connection for the gateway.
func ConfigureGatewayDeployment(
	d *appsv1.Deployment,
	mode lokiv1.ModeType,
	secretVolumeName, tlsDir string,
	minTLSVersion, ciphers string,
	withTLS bool, adminGroups []string,
) error {
	p := corev1.PodSpec{
		ServiceAccountName: d.GetName(),
		Containers: []corev1.Container{
			newOPAOpenShiftContainer(mode, secretVolumeName, tlsDir, minTLSVersion, ciphers, withTLS, adminGroups),
		},
	}

	if err := mergo.Merge(&d.Spec.Template.Spec, p, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge sidecar container spec ")
	}

	if mode == lokiv1.OpenshiftLogging {
		// enable extraction of namespace selector
		for i, c := range d.Spec.Template.Spec.Containers {
			if c.Name != "gateway" {
				continue
			}

			d.Spec.Template.Spec.Containers[i].Args = append(d.Spec.Template.Spec.Containers[i].Args,
				fmt.Sprintf("--logs.auth.extract-selectors=%s", opaDefaultLabelMatcher),
			)
		}
	}

	return nil
}

// ConfigureGatewayDeploymentRulesAPI merges CLI argument to the gateway container
// that allow only Rules API access with a valid namespace input for the tenant application.
func ConfigureGatewayDeploymentRulesAPI(d *appsv1.Deployment, containerName string) error {
	var gwIndex int
	for i, c := range d.Spec.Template.Spec.Containers {
		if c.Name == containerName {
			gwIndex = i
			break
		}
	}

	container := corev1.Container{
		Args: []string{
			fmt.Sprintf("--logs.rules.label-filters=%s:%s", tenantApplication, opaDefaultLabelMatcher),
		},
	}

	if err := mergo.Merge(&d.Spec.Template.Spec.Containers[gwIndex], container, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge container")
	}

	return nil
}

// ConfigureGatewayService merges the OpenPolicyAgent sidecar metrics port into
// the service spec. With this the metrics are exposed through the same service.
func ConfigureGatewayService(s *corev1.ServiceSpec) error {
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

// ConfigureGatewayServiceMonitor merges the OpenPolicyAgent sidecar endpoint into
// the service monitor. With this cluster-monitoring prometheus can scrape
// the sidecar metrics.
func ConfigureGatewayServiceMonitor(sm *monitoringv1.ServiceMonitor, withTLS bool) error {
	var opaEndpoint monitoringv1.Endpoint

	if withTLS {
		authn := sm.Spec.Endpoints[0].Authorization
		tlsConfig := sm.Spec.Endpoints[0].TLSConfig

		opaEndpoint = monitoringv1.Endpoint{
			Port:          opaMetricsPortName,
			Path:          "/metrics",
			Scheme:        "https",
			Authorization: authn,
			TLSConfig:     tlsConfig,
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

// ConfigureRulerStatefulSet configures the ruler to use the cluster monitoring alertmanager.
func ConfigureRulerStatefulSet(
	ss *appsv1.StatefulSet,
	alertmanagerCABundleName string,
	token, caDir, caPath string,
	monitorServerName, rulerContainerName string,
) error {
	var rulerIndex int
	for i, c := range ss.Spec.Template.Spec.Containers {
		if c.Name == rulerContainerName {
			rulerIndex = i
			break
		}
	}

	secretVolumeSpec := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: alertmanagerCABundleName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						DefaultMode: &defaultConfigMapMode,
						LocalObjectReference: corev1.LocalObjectReference{
							Name: alertmanagerCABundleName,
						},
					},
				},
			},
		},
	}

	rulerContainer := ss.Spec.Template.Spec.Containers[rulerIndex].DeepCopy()

	rulerContainer.Args = append(rulerContainer.Args,
		fmt.Sprintf("-ruler.alertmanager-client.tls-ca-path=%s", caPath),
		fmt.Sprintf("-ruler.alertmanager-client.tls-server-name=%s", monitorServerName),
		fmt.Sprintf("-ruler.alertmanager-client.credentials-file=%s", token),
	)

	rulerContainer.VolumeMounts = append(rulerContainer.VolumeMounts, corev1.VolumeMount{
		Name:      alertmanagerCABundleName,
		ReadOnly:  true,
		MountPath: caDir,
	})

	p := corev1.PodSpec{
		ServiceAccountName: ss.GetName(),
		Containers: []corev1.Container{
			*rulerContainer,
		},
	}

	if err := mergo.Merge(&ss.Spec.Template.Spec, secretVolumeSpec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge volumes")
	}

	if err := mergo.Merge(&ss.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge ruler container spec ")
	}

	return nil
}

// ConfigureOptions applies default configuration for the use of the cluster monitoring alertmanager.
func ConfigureOptions(configOpt *config.Options, am, uwam bool, token, caPath, monitorServerName string) error {
	if am {
		err := configureDefaultMonitoringAM(configOpt)
		if err != nil {
			return err
		}
	}

	if uwam {
		err := configureUserWorkloadAM(configOpt, token, caPath, monitorServerName)
		if err != nil {
			return err
		}
	}

	return nil
}

func configureDefaultMonitoringAM(configOpt *config.Options) error {
	if configOpt.Ruler.AlertManager == nil {
		configOpt.Ruler.AlertManager = &config.AlertManagerConfig{}
	}

	if len(configOpt.Ruler.AlertManager.Hosts) == 0 {
		amc := &config.AlertManagerConfig{
			Hosts:           fmt.Sprintf("https://_web._tcp.%s.%s.svc", MonitoringSVCOperated, monitoringNamespace),
			EnableV2:        true,
			EnableDiscovery: true,
			RefreshInterval: "1m",
		}

		if err := mergo.Merge(configOpt.Ruler.AlertManager, amc); err != nil {
			return kverrors.Wrap(err, "failed merging AlertManager config")
		}
	}

	return nil
}

func configureUserWorkloadAM(configOpt *config.Options, token, caPath, monitorServerName string) error {
	if configOpt.Overrides == nil {
		configOpt.Overrides = map[string]config.LokiOverrides{}
	}

	lokiOverrides := configOpt.Overrides[tenantApplication]

	if lokiOverrides.Ruler.AlertManager != nil {
		return nil
	}

	lokiOverrides.Ruler.AlertManager = &config.AlertManagerConfig{
		Hosts:           fmt.Sprintf("https://_web._tcp.%s.%s.svc", MonitoringSVCOperated, MonitoringUserWorkloadNS),
		EnableV2:        true,
		EnableDiscovery: true,
		RefreshInterval: "1m",
		Notifier: &config.NotifierConfig{
			TLS: config.TLSConfig{
				ServerName: ptr.To(monitorServerName),
				CAPath:     &caPath,
			},
			HeaderAuth: config.HeaderAuth{
				CredentialsFile: &token,
				Type:            ptr.To("Bearer"),
			},
		},
	}

	configOpt.Overrides[tenantApplication] = lokiOverrides
	return nil
}

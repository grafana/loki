package openshift

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/imdario/mergo"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
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

	logsEndpointRe = regexp.MustCompile(`.*logs..*.endpoint.*`)
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
	gwContainerName string,
	secretVolumeName, tlsDir, certFile, keyFile string,
	caBundleVolumeName, caDir, caFile string,
	withTLS, withCertSigningService bool,
	secretName, serverName string,
	gatewayHTTPPort int,
	minTLSVersion string,
	ciphers string,
) error {
	var gwIndex int
	for i, c := range d.Spec.Template.Spec.Containers {
		if c.Name == gwContainerName {
			gwIndex = i
			break
		}
	}

	gwContainer := d.Spec.Template.Spec.Containers[gwIndex].DeepCopy()
	gwArgs := gwContainer.Args
	gwVolumes := d.Spec.Template.Spec.Volumes

	if withCertSigningService {
		for i, a := range gwArgs {
			if logsEndpointRe.MatchString(a) {
				gwContainer.Args[i] = strings.Replace(a, "http", "https", 1)
			}
		}

		gwArgs = append(gwArgs, fmt.Sprintf("--logs.tls.ca-file=%s/%s", caDir, caFile))

		gwContainer.VolumeMounts = append(gwContainer.VolumeMounts, corev1.VolumeMount{
			Name:      caBundleVolumeName,
			ReadOnly:  true,
			MountPath: caDir,
		})

		gwVolumes = append(gwVolumes, corev1.Volume{
			Name: caBundleVolumeName,
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					DefaultMode: &defaultConfigMapMode,
					LocalObjectReference: corev1.LocalObjectReference{
						Name: caBundleVolumeName,
					},
				},
			},
		})
	}

	for i, a := range gwArgs {
		if strings.HasPrefix(a, "--web.healthchecks.url=") {
			gwArgs[i] = fmt.Sprintf("--web.healthchecks.url=https://localhost:%d", gatewayHTTPPort)
			break
		}
	}

	certFilePath := path.Join(tlsDir, certFile)
	keyFilePath := path.Join(tlsDir, keyFile)
	caFilePath := path.Join(caDir, caFile)
	gwArgs = append(gwArgs,
		"--tls.client-auth-type=NoClientCert",
		fmt.Sprintf("--tls.server.cert-file=%s", certFilePath),
		fmt.Sprintf("--tls.server.key-file=%s", keyFilePath),
		fmt.Sprintf("--tls.healthchecks.server-ca-file=%s", caFilePath),
		fmt.Sprintf("--tls.healthchecks.server-name=%s", serverName))

	gwContainer.ReadinessProbe.ProbeHandler.HTTPGet.Scheme = corev1.URISchemeHTTPS
	gwContainer.LivenessProbe.ProbeHandler.HTTPGet.Scheme = corev1.URISchemeHTTPS

	// Create and mount TLS secrets volumes if not already created.
	if !withTLS {
		gwVolumes = append(gwVolumes, corev1.Volume{
			Name: secretVolumeName,
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
				},
			},
		})

		gwContainer.VolumeMounts = append(gwContainer.VolumeMounts, corev1.VolumeMount{
			Name:      secretVolumeName,
			ReadOnly:  true,
			MountPath: tlsDir,
		})

		// Add TLS profile info args since openshift gateway always uses TLS.
		gwArgs = append(gwArgs,
			fmt.Sprintf("--tls.min-version=%s", minTLSVersion),
			fmt.Sprintf("--tls.cipher-suites=%s", ciphers))
	}

	gwContainer.Args = gwArgs

	p := corev1.PodSpec{
		ServiceAccountName: d.GetName(),
		Containers: []corev1.Container{
			*gwContainer,
			newOPAOpenShiftContainer(mode, secretVolumeName, tlsDir, certFile, keyFile, minTLSVersion, ciphers, withTLS),
		},
		Volumes: gwVolumes,
	}

	if err := mergo.Merge(&d.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge sidecar container spec ")
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

// ConfigureQueryFrontendDeployment configures use of TLS when enabled.
func ConfigureQueryFrontendDeployment(
	d *appsv1.Deployment,
	proxyURL string,
	qfContainerName string,
	caBundleVolumeName, caDir, caFile string,
) error {
	var qfIdx int
	for i, c := range d.Spec.Template.Spec.Containers {
		if c.Name == qfContainerName {
			qfIdx = i
			break
		}
	}

	containerSpec := corev1.Container{
		Args: []string{
			fmt.Sprintf("-frontend.tail-proxy-url=%s", proxyURL),
			fmt.Sprintf("-frontend.tail-tls-config.tls-ca-path=%s/%s", caDir, caFile),
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      caBundleVolumeName,
				ReadOnly:  true,
				MountPath: caDir,
			},
		},
	}

	p := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: caBundleVolumeName,
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						DefaultMode: &defaultConfigMapMode,
						LocalObjectReference: corev1.LocalObjectReference{
							Name: caBundleVolumeName,
						},
					},
				},
			},
		},
	}

	if err := mergo.Merge(&d.Spec.Template.Spec.Containers[qfIdx], containerSpec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to add tls config args")
	}

	if err := mergo.Merge(&d.Spec.Template.Spec, p, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to add tls volumes")
	}

	return nil
}

// ConfigureRulerStatefulSet configures the ruler to use the cluster monitoring alertmanager.
func ConfigureRulerStatefulSet(
	ss *appsv1.StatefulSet,
	token, caBundleVolumeName, caDir, caFile string,
	monitorServerName, rulerContainerName string,
) error {
	var rulerIndex int
	for i, c := range ss.Spec.Template.Spec.Containers {
		if c.Name == rulerContainerName {
			rulerIndex = i
			break
		}
	}

	rulerContainer := ss.Spec.Template.Spec.Containers[rulerIndex].DeepCopy()

	rulerContainer.Args = append(rulerContainer.Args,
		fmt.Sprintf("-ruler.alertmanager-client.tls-ca-path=%s/%s", caDir, caFile),
		fmt.Sprintf("-ruler.alertmanager-client.tls-server-name=%s", monitorServerName),
		fmt.Sprintf("-ruler.alertmanager-client.credentials-file=%s", token),
	)

	p := corev1.PodSpec{
		ServiceAccountName: ss.GetName(),
		Containers: []corev1.Container{
			*rulerContainer,
		},
	}

	if err := mergo.Merge(&ss.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge ruler container spec ")
	}

	return nil
}

// ConfigureOptions applies default configuration for the use of the cluster monitoring alertmanager.
func ConfigureOptions(configOpt *config.Options) error {
	if configOpt.Ruler.AlertManager == nil {
		configOpt.Ruler.AlertManager = &config.AlertManagerConfig{}
	}

	if len(configOpt.Ruler.AlertManager.Hosts) == 0 {
		amc := &config.AlertManagerConfig{
			Hosts:           "https://_web._tcp.alertmanager-operated.openshift-monitoring.svc",
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

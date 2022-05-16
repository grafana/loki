package openshift

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/imdario/mergo"

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
)

var (
	// defaultTenants represents the slice of all supported LokiStack on OpenShift.
	defaultTenants = []string{
		tenantApplication,
		tenantInfrastructure,
		tenantAudit,
	}

	logsEndpointRe = regexp.MustCompile(`.*logs..*.endpoint.*`)
)

// ConfigureGatewayDeployment merges an OpenPolicyAgent sidecar into the deployment spec.
// With this, the deployment will route authorization request to the OpenShift
// apiserver through the sidecar.
func ConfigureGatewayDeployment(
	d *appsv1.Deployment,
	gwContainerName string,
	sercretVolumeName, tlsDir, certFile, keyFile string,
	caDir, caFile string,
	withTLS, withCertSigningService bool,
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

		caBundleVolumeName := serviceCABundleName(Options{
			BuildOpts: BuildOptions{
				GatewayName: d.GetName(),
			},
		})

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

	gwContainer.Args = gwArgs

	p := corev1.PodSpec{
		ServiceAccountName: d.GetName(),
		Containers: []corev1.Container{
			*gwContainer,
			newOPAOpenShiftContainer(sercretVolumeName, tlsDir, certFile, keyFile, withTLS),
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

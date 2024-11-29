package openshift

import (
	"fmt"
	"os"
	"path"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

const (
	envRelatedImageOPA        = "RELATED_IMAGE_OPA"
	defaultOPAImage           = "quay.io/observatorium/opa-openshift:latest"
	opaContainerName          = "opa"
	opaDefaultPackage         = "lokistack"
	opaDefaultAPIGroup        = "loki.grafana.com"
	opaMetricsPortName        = "opa-metrics"
	opaDefaultLabelMatcher    = "kubernetes_namespace_name"
	opaNetworkLabelMatchers   = "SrcK8S_Namespace,DstK8S_Namespace"
	ocpMonitoringGroupByLabel = "namespace"
)

func newOPAOpenShiftContainer(mode lokiv1.ModeType, secretVolumeName, tlsDir, minTLSVersion, ciphers string, withTLS bool, adminGroups []string) corev1.Container {
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
		fmt.Sprintf("--web.listen=:%d", GatewayOPAHTTPPort),
		fmt.Sprintf("--web.internal.listen=:%d", GatewayOPAInternalPort),
		fmt.Sprintf("--web.healthchecks.url=http://localhost:%d", GatewayOPAHTTPPort),
		"--opa.skip-tenants=audit,infrastructure",
		fmt.Sprintf("--opa.package=%s", opaDefaultPackage),
	}

	if len(adminGroups) > 0 {
		args = append(args, fmt.Sprintf("--opa.admin-groups=%s", strings.Join(adminGroups, ",")))
	}

	if mode != lokiv1.OpenshiftNetwork {
		args = append(args, []string{
			fmt.Sprintf("--opa.matcher=%s", opaDefaultLabelMatcher),
		}...)
	} else {
		args = append(args, []string{
			fmt.Sprintf("--opa.matcher=%s", opaNetworkLabelMatchers),
			"--opa.matcher-op=or",
		}...)
	}

	if withTLS {
		certFilePath := path.Join(tlsDir, corev1.TLSCertKey)
		keyFilePath := path.Join(tlsDir, corev1.TLSPrivateKeyKey)

		args = append(args, []string{
			fmt.Sprintf("--tls.internal.server.cert-file=%s", certFilePath),
			fmt.Sprintf("--tls.internal.server.key-file=%s", keyFilePath),
			fmt.Sprintf("--tls.min-version=%s", minTLSVersion),
			fmt.Sprintf("--tls.cipher-suites=%s", ciphers),
		}...)

		uriScheme = corev1.URISchemeHTTPS

		volumeMounts = []corev1.VolumeMount{
			{
				Name:      secretVolumeName,
				ReadOnly:  true,
				MountPath: tlsDir,
			},
		}
	}

	tenants := GetTenants(mode)
	for _, t := range tenants {
		args = append(args, fmt.Sprintf(`--openshift.mappings=%s=%s`, t, opaDefaultAPIGroup))
	}

	return corev1.Container{
		Name:  opaContainerName,
		Image: image,
		Args:  args,
		Ports: []corev1.ContainerPort{
			{
				Name:          GatewayOPAHTTPPortName,
				ContainerPort: GatewayOPAHTTPPort,
				Protocol:      corev1.ProtocolTCP,
			},
			{
				Name:          GatewayOPAInternalPortName,
				ContainerPort: GatewayOPAInternalPort,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path:   "/live",
					Port:   intstr.FromInt(int(GatewayOPAInternalPort)),
					Scheme: uriScheme,
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
					Port:   intstr.FromInt(int(GatewayOPAInternalPort)),
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

package openshift

import (
	"fmt"
	"os"
	"path"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	envRelatedImageOPA = "RELATED_IMAGE_OPA"
	defaultOPAImage    = "quay.io/observatorium/opa-openshift:latest"
	opaContainerName   = "opa"
	opaDefaultPackage  = "lokistack"
	opaDefaultAPIGroup = "loki.openshift.io"
	opaMetricsPortName = "opa-metrics"
)

func newOPAOpenShiftContainer(sercretVolumeName, tlsDir, certFile, keyFile string, withTLS bool) corev1.Container {
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
		fmt.Sprintf("--web.listen=:%d", GatewayOPAHTTPPort),
		fmt.Sprintf("--web.internal.listen=:%d", GatewayOPAInternalPort),
		fmt.Sprintf("--web.healthchecks.url=http://localhost:%d", GatewayOPAHTTPPort),
	}

	if withTLS {
		certFilePath := path.Join(tlsDir, certFile)
		keyFilePath := path.Join(tlsDir, keyFile)

		args = append(args, []string{
			fmt.Sprintf("--tls.internal.server.cert-file=%s", certFilePath),
			fmt.Sprintf("--tls.internal.server.key-file=%s", keyFilePath),
		}...)

		uriScheme = corev1.URISchemeHTTPS

		volumeMounts = []corev1.VolumeMount{
			{
				Name:      sercretVolumeName,
				ReadOnly:  true,
				MountPath: tlsDir,
			},
		}
	}

	for _, t := range defaultTenants {
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
			Handler: corev1.Handler{
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
			Handler: corev1.Handler{
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

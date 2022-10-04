package manifests

import (
	"fmt"
	"path"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/imdario/mergo"
	corev1 "k8s.io/api/core/v1"
)

func configureGRPCServicePKI(podSpec *corev1.PodSpec, serviceName string) error {
	secretVolumeSpec := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: serviceName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: serviceName,
					},
				},
			},
		},
	}
	secretContainerSpec := corev1.Container{
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      serviceName,
				ReadOnly:  false,
				MountPath: grpcTLSDir,
			},
		},
		Args: []string{
			fmt.Sprintf("-server.grpc-tls-cert-path=%s", path.Join(grpcTLSDir, tlsCertFile)),
			fmt.Sprintf("-server.grpc-tls-key-path=%s", path.Join(grpcTLSDir, tlsKeyFile)),
		},
	}

	if err := mergo.Merge(podSpec, secretVolumeSpec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge volumes")
	}

	if err := mergo.Merge(&podSpec.Containers[0], secretContainerSpec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge container")
	}

	return nil
}

func configureHTTPServicePKI(podSpec *corev1.PodSpec, serviceName string) error {
	secretVolumeSpec := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: serviceName,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: serviceName,
					},
				},
			},
		},
	}
	secretContainerSpec := corev1.Container{
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      serviceName,
				ReadOnly:  false,
				MountPath: httpTLSDir,
			},
		},
		Args: []string{
			fmt.Sprintf("-server.http-tls-cert-path=%s", path.Join(httpTLSDir, tlsCertFile)),
			fmt.Sprintf("-server.http-tls-key-path=%s", path.Join(httpTLSDir, tlsKeyFile)),
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

	if err := mergo.Merge(&podSpec.Containers[0], secretContainerSpec, mergo.WithAppendSlice); err != nil {
		return kverrors.Wrap(err, "failed to merge container")
	}

	if err := mergo.Merge(&podSpec.Containers[0], uriSchemeContainerSpec, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge container")
	}

	return nil
}

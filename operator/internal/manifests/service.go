package manifests

import (
	"fmt"
	"path"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/imdario/mergo"
	corev1 "k8s.io/api/core/v1"
)

func configureGRPCServicePKI(podSpec *corev1.PodSpec, serviceName string) error {
	secretName := signingServiceSecretName(serviceName)
	secretVolumeSpec := corev1.PodSpec{
		Volumes: []corev1.Volume{
			{
				Name: secretName,
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
				Name:      secretName,
				ReadOnly:  false,
				MountPath: grpcSecretDirectory,
			},
		},
		Args: []string{
			fmt.Sprintf("-server.grpc-tls-cert-path=%s", path.Join(grpcSecretDirectory, "tls.crt")),
			fmt.Sprintf("-server.grpc-tls-key-path=%s", path.Join(grpcSecretDirectory, "tls.key")),
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

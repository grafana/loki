package storage

import (
	"path"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/imdario/mergo"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

const (
	// EnvGoogleApplicationCredentials is the environment variable to specify path to key.json
	EnvGoogleApplicationCredentials = "GOOGLE_APPLICATION_CREDENTIALS"
	// GCSFileName is the file containing the Google credentials for authentication
	GCSFileName     = "key.json"
	secretDirectory = "/etc/proxy/secrets"
)

// ConfigureDeployment merges a GCS Object Storage volume into the deployment spec.
// With this, the deployment will expose an environment variable for Google authentication.
func ConfigureDeployment(d *appsv1.Deployment, secretName string) error {
	p := ensureCredentialsForGCS(&d.Spec.Template.Spec, secretName)

	if err := mergo.Merge(&d.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge gcs object storage spec ")
	}

	return nil
}

// ConfigureStatefulSet merges a GCS Object Storage volume into the statefulset spec.
// With this, the statefulset will expose an environment variable for Google authentication.
func ConfigureStatefulSet(s *appsv1.StatefulSet, secretName string) error {
	p := ensureCredentialsForGCS(&s.Spec.Template.Spec, secretName)

	if err := mergo.Merge(&s.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge gcs object storage spec ")
	}

	return nil
}

func ensureCredentialsForGCS(p *corev1.PodSpec, secretName string) corev1.PodSpec {
	container := p.Containers[0].DeepCopy()
	volumes := p.Volumes

	volumes = append(volumes, corev1.Volume{
		Name: secretName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: secretName,
			},
		},
	})

	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      secretName,
		ReadOnly:  false,
		MountPath: secretDirectory,
	})

	container.Env = append(container.Env, corev1.EnvVar{
		Name:  EnvGoogleApplicationCredentials,
		Value: path.Join(secretDirectory, GCSFileName),
	})

	return corev1.PodSpec{
		Containers: []corev1.Container{
			*container,
		},
		Volumes: volumes,
	}
}

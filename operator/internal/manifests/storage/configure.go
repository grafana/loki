package storage

import (
	"fmt"
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
	GCSFileName = "key.json"

	secretDirectory = "/etc/storage/secrets"
	caDirectory     = "/var/run/ca"
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

// ConfigureDeploymentCA merges a S3 CA ConfigMap volume into the deployment spec.
func ConfigureDeploymentCA(d *appsv1.Deployment, cmName string) error {
	p := ensureCAForS3(&d.Spec.Template.Spec, cmName)

	if err := mergo.Merge(&d.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge s3 object storage ca options ")
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

// ConfigureStatefulSetCA merges a S3 CA ConfigMap volume into the statefulset spec.
func ConfigureStatefulSetCA(s *appsv1.StatefulSet, cmName string) error {
	p := ensureCAForS3(&s.Spec.Template.Spec, cmName)

	if err := mergo.Merge(&s.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge s3 object storage ca options ")
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

func ensureCAForS3(p *corev1.PodSpec, cmName string) corev1.PodSpec {
	container := p.Containers[0].DeepCopy()
	volumes := p.Volumes

	volumes = append(volumes, corev1.Volume{
		Name: cmName,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: cmName,
				},
			},
		},
	})

	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      cmName,
		ReadOnly:  false,
		MountPath: caDirectory,
	})

	container.Args = append(container.Args,
		fmt.Sprintf("-s3.http.ca-file=%s", path.Join(caDirectory, "ca.crt")),
	)

	return corev1.PodSpec{
		Containers: []corev1.Container{
			*container,
		},
		Volumes: volumes,
	}
}

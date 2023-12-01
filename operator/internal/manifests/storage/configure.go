package storage

import (
	"fmt"
	"path"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/imdario/mergo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
)

const (
	// EnvAlibabaCloudAccessKeyID is the environment variable to specify the AlibabaCloud client id to access S3.
	EnvAlibabaCloudAccessKeyID = "ALIBABA_CLOUD_ACCESS_KEY_ID"
	// EnvAlibabaCloudAccessKeySecret is the environment variable to specify the AlibabaCloud client secret to access S3.
	EnvAlibabaCloudAccessKeySecret = "ALIBABA_CLOUD_ACCESS_KEY_SECRET"
	// EnvAWSAccessKeyID is the environment variable to specify the AWS client id to access S3.
	EnvAWSAccessKeyID = "AWS_ACCESS_KEY_ID"
	// EnvAWSAccessKeySecre is the environment variable to specify the AWS client secret to access S3.
	EnvAWSAccessKeySecret = "AWS_ACCESS_KEY_SECRET"
	// EnvAzureStorageAccountName is the environment variable to specify the Azure storage account name to access the container.
	EnvAzureStorageAccountName = "AZURE_ACCOUNT_NAME"
	// EnvAzureStorageAccountKey is the environment variable to specify the Azure storage account key to access the container.
	EnvAzureStorageAccountKey = "AZURE_ACCOUNT_KEY"
	// EnvGoogleApplicationCredentials is the environment variable to specify path to key.json
	EnvGoogleApplicationCredentials = "GOOGLE_APPLICATION_CREDENTIALS"
	// GCSFileName is the file containing the Google credentials for authentication
	GCSFileName = "key.json"

	secretDirectory  = "/etc/storage/secrets"
	storageTLSVolume = "storage-tls"
	caDirectory      = "/etc/storage/ca"
)

// ConfigureDeployment appends additional pod volumes and container env vars, args, volume mounts
// based on the object storage type. Currently supported amendments:
// - GCS: Ensure env var GOOGLE_APPLICATION_CREDENTIALS in container
// - S3: Ensure mounting custom CA configmap if any TLSConfig given
func ConfigureDeployment(d *appsv1.Deployment, opts Options) error {
	switch opts.SharedStore {
	case lokiv1.ObjectStorageSecretAlibabaCloud, lokiv1.ObjectStorageSecretAzure, lokiv1.ObjectStorageSecretGCS:
		return configureDeployment(d, opts.SecretName, opts.SharedStore)
	case lokiv1.ObjectStorageSecretS3:
		if err := configureDeployment(d, opts.SecretName, opts.SharedStore); err != nil {
			return err
		}
		if opts.TLS == nil {
			return nil
		}
		return configureDeploymentCA(d, opts.TLS)
	default:
		return nil
	}
}

// ConfigureStatefulSet appends additional pod volumes and container env vars, args, volume mounts
// based on the object storage type. Currently supported amendments:
// - GCS: Ensure env var GOOGLE_APPLICATION_CREDENTIALS in container
// - S3: Ensure mounting custom CA configmap if any TLSConfig given
func ConfigureStatefulSet(d *appsv1.StatefulSet, opts Options) error {
	switch opts.SharedStore {
	case lokiv1.ObjectStorageSecretAlibabaCloud, lokiv1.ObjectStorageSecretAzure, lokiv1.ObjectStorageSecretGCS:
		return configureStatefulSet(d, opts.SecretName, opts.SharedStore)
	case lokiv1.ObjectStorageSecretS3:
		if err := configureStatefulSet(d, opts.SecretName, opts.SharedStore); err != nil {
			return err
		}
		if opts.TLS == nil {
			return nil
		}
		return configureStatefulSetCA(d, opts.TLS)
	default:
		return nil
	}
}

// ConfigureDeployment merges the object storage secret volume into the deployment spec.
// With this, the deployment will expose credentials specific environment variables.
func configureDeployment(d *appsv1.Deployment, secretName string, t lokiv1.ObjectStorageSecretType) error {
	p := ensureObjectStoreCredentials(&d.Spec.Template.Spec, secretName, t)

	if err := mergo.Merge(&d.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge gcs object storage spec ")
	}

	return nil
}

// ConfigureDeploymentCA merges a S3 CA ConfigMap volume into the deployment spec.
func configureDeploymentCA(d *appsv1.Deployment, tls *TLSConfig) error {
	p := ensureCAForS3(&d.Spec.Template.Spec, tls)

	if err := mergo.Merge(&d.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge s3 object storage ca options ")
	}

	return nil
}

// ConfigureStatefulSet merges a the object storage secrect volume into the statefulset spec.
// With this, the statefulset will expose credentials specific environment variable.
func configureStatefulSet(s *appsv1.StatefulSet, secretName string, t lokiv1.ObjectStorageSecretType) error {
	p := ensureObjectStoreCredentials(&s.Spec.Template.Spec, secretName, t)

	if err := mergo.Merge(&s.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge gcs object storage spec ")
	}

	return nil
}

// ConfigureStatefulSetCA merges a S3 CA ConfigMap volume into the statefulset spec.
func configureStatefulSetCA(s *appsv1.StatefulSet, tls *TLSConfig) error {
	p := ensureCAForS3(&s.Spec.Template.Spec, tls)

	if err := mergo.Merge(&s.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge s3 object storage ca options ")
	}

	return nil
}

func ensureObjectStoreCredentials(p *corev1.PodSpec, secretName string, t lokiv1.ObjectStorageSecretType) corev1.PodSpec {
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

	var objStoreEnvVar []corev1.EnvVar
	switch t {
	case lokiv1.ObjectStorageSecretAlibabaCloud:
		objStoreEnvVar = []corev1.EnvVar{
			{
				Name: EnvAlibabaCloudAccessKeyID,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: secretName,
						},
						Key: "access_key_id", // TODO(@periklis): make this a constant
					},
				},
			},
			{
				Name: EnvAlibabaCloudAccessKeySecret,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: secretName,
						},
						Key: "secret_access_key", // TODO(@periklis): make this a constant
					},
				},
			},
		}
	case lokiv1.ObjectStorageSecretAzure:
		objStoreEnvVar = []corev1.EnvVar{
			{
				Name: EnvAzureStorageAccountName,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: secretName,
						},
						Key: "account_name", // TODO(@periklis): make this a constant
					},
				},
			},
			{
				Name: EnvAzureStorageAccountKey,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: secretName,
						},
						Key: "account_key", // TODO(@periklis): make this a constant
					},
				},
			},
		}
	case lokiv1.ObjectStorageSecretGCS:
		objStoreEnvVar = []corev1.EnvVar{
			{
				Name:  EnvGoogleApplicationCredentials,
				Value: path.Join(secretDirectory, GCSFileName),
			},
		}
	case lokiv1.ObjectStorageSecretS3:
		objStoreEnvVar = []corev1.EnvVar{
			{
				Name: EnvAWSAccessKeyID,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: secretName,
						},
						Key: "access_key_id", // TODO(@periklis): make this a constant
					},
				},
			},
			{
				Name: EnvAWSAccessKeySecret,
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: secretName,
						},
						Key: "access_key_secret", // TODO(@periklis): make this a constant
					},
				},
			},
		}
	}

	container.Env = append(container.Env, objStoreEnvVar...)

	return corev1.PodSpec{
		Containers: []corev1.Container{
			*container,
		},
		Volumes: volumes,
	}
}

func ensureCAForS3(p *corev1.PodSpec, tls *TLSConfig) corev1.PodSpec {
	container := p.Containers[0].DeepCopy()
	volumes := p.Volumes

	volumes = append(volumes, corev1.Volume{
		Name: storageTLSVolume,
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: tls.CA,
				},
			},
		},
	})

	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{
		Name:      storageTLSVolume,
		ReadOnly:  false,
		MountPath: caDirectory,
	})

	container.Args = append(container.Args,
		fmt.Sprintf("-s3.http.ca-file=%s", path.Join(caDirectory, tls.Key)),
	)

	return corev1.PodSpec{
		Containers: []corev1.Container{
			*container,
		},
		Volumes: volumes,
	}
}

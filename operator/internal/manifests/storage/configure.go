package storage

import (
	"fmt"
	"path"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/imdario/mergo"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

var (
	tokenCCOAuthConfigVolumeMount = corev1.VolumeMount{
		Name:      tokenAuthConfigVolumeName,
		MountPath: tokenAuthConfigDirectory,
	}

	saTokenVolumeMount = corev1.VolumeMount{
		Name:      saTokenVolumeName,
		MountPath: saTokenVolumeMountPath,
	}
)

// ConfigureDeployment appends additional pod volumes and container env vars, args, volume mounts
// based on the object storage type. Currently supported amendments:
// - All: Ensure object storage secret mounted and auth projected as env vars.
// - GCS: Ensure env var GOOGLE_APPLICATION_CREDENTIALS in container
// - S3 & Swift: Ensure mounting custom CA configmap if any TLSConfig given
func ConfigureDeployment(d *appsv1.Deployment, opts Options) error {
	switch opts.SharedStore {
	case lokiv1.ObjectStorageSecretAlibabaCloud, lokiv1.ObjectStorageSecretAzure, lokiv1.ObjectStorageSecretGCS:
		return configureDeployment(d, opts)
	case lokiv1.ObjectStorageSecretS3:
		err := configureDeployment(d, opts)
		if err != nil {
			return err
		}
		return configureDeploymentCA(d, opts.TLS, lokiv1.ObjectStorageSecretS3)
	case lokiv1.ObjectStorageSecretSwift:
		err := configureDeployment(d, opts)
		if err != nil {
			return err
		}
		return configureDeploymentCA(d, opts.TLS, lokiv1.ObjectStorageSecretSwift)
	default:
		return nil
	}
}

// ConfigureStatefulSet appends additional pod volumes and container env vars, args, volume mounts
// based on the object storage type. Currently supported amendments:
// - All: Ensure object storage secret mounted and auth projected as env vars.
// - GCS: Ensure env var GOOGLE_APPLICATION_CREDENTIALS in container
// - S3 & Swift: Ensure mounting custom CA configmap if any TLSConfig given
func ConfigureStatefulSet(d *appsv1.StatefulSet, opts Options) error {
	switch opts.SharedStore {
	case lokiv1.ObjectStorageSecretAlibabaCloud, lokiv1.ObjectStorageSecretAzure, lokiv1.ObjectStorageSecretGCS:
		return configureStatefulSet(d, opts)
	case lokiv1.ObjectStorageSecretS3:
		if err := configureStatefulSet(d, opts); err != nil {
			return err
		}
		return configureStatefulSetCA(d, opts.TLS, lokiv1.ObjectStorageSecretS3)
	case lokiv1.ObjectStorageSecretSwift:
		if err := configureStatefulSet(d, opts); err != nil {
			return err
		}
		return configureStatefulSetCA(d, opts.TLS, lokiv1.ObjectStorageSecretSwift)
	default:
		return nil
	}
}

// ConfigureDeployment merges the object storage secret volume into the deployment spec.
// With this, the deployment will expose credentials specific environment variables.
func configureDeployment(d *appsv1.Deployment, opts Options) error {
	p := ensureObjectStoreCredentials(&d.Spec.Template.Spec, opts)
	if err := mergo.Merge(&d.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge gcs object storage spec ")
	}

	return nil
}

// ConfigureDeploymentCA merges a S3 or Swift CA ConfigMap volume into the deployment spec.
func configureDeploymentCA(d *appsv1.Deployment, tls *TLSConfig, secretType lokiv1.ObjectStorageSecretType) error {
	if tls == nil {
		return nil
	}

	var p corev1.PodSpec
	switch secretType {
	case lokiv1.ObjectStorageSecretS3:
		p = ensureCAForObjectStorage(&d.Spec.Template.Spec, tls, lokiv1.ObjectStorageSecretS3)
	case lokiv1.ObjectStorageSecretSwift:
		p = ensureCAForObjectStorage(&d.Spec.Template.Spec, tls, lokiv1.ObjectStorageSecretSwift)
	}

	if err := mergo.Merge(&d.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge object storage ca options ")
	}

	return nil
}

// ConfigureStatefulSet merges a the object storage secrect volume into the statefulset spec.
// With this, the statefulset will expose credentials specific environment variable.
func configureStatefulSet(s *appsv1.StatefulSet, opts Options) error {
	p := ensureObjectStoreCredentials(&s.Spec.Template.Spec, opts)
	if err := mergo.Merge(&s.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge gcs object storage spec ")
	}

	return nil
}

// ConfigureStatefulSetCA merges a S3 or Swift CA ConfigMap volume into the statefulset spec.
func configureStatefulSetCA(s *appsv1.StatefulSet, tls *TLSConfig, secretType lokiv1.ObjectStorageSecretType) error {
	if tls == nil {
		return nil
	}
	var p corev1.PodSpec

	switch secretType {
	case lokiv1.ObjectStorageSecretS3:
		p = ensureCAForObjectStorage(&s.Spec.Template.Spec, tls, lokiv1.ObjectStorageSecretS3)
	case lokiv1.ObjectStorageSecretSwift:
		p = ensureCAForObjectStorage(&s.Spec.Template.Spec, tls, lokiv1.ObjectStorageSecretSwift)
	}

	if err := mergo.Merge(&s.Spec.Template.Spec, p, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge object storage ca options ")
	}

	return nil
}

func ensureObjectStoreCredentials(p *corev1.PodSpec, opts Options) corev1.PodSpec {
	container := p.Containers[0].DeepCopy()
	volumes := p.Volumes
	secretName := opts.SecretName

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

	if tokenAuthEnabled(opts) {
		container.Env = append(container.Env, tokenAuthCredentials(opts)...)
		volumes = append(volumes, saTokenVolume(opts))
		container.VolumeMounts = append(container.VolumeMounts, saTokenVolumeMount)

		isSTS := opts.S3 != nil && opts.S3.STS
		isWIF := opts.GCS != nil && opts.GCS.WorkloadIdentity
		if opts.OpenShift.TokenCCOAuthEnabled() && (isSTS || isWIF) {
			volumes = append(volumes, tokenCCOAuthConfigVolume(opts))
			container.VolumeMounts = append(container.VolumeMounts, tokenCCOAuthConfigVolumeMount)
		}
	} else {
		container.Env = append(container.Env, staticAuthCredentials(opts)...)
	}
	container.Env = append(container.Env, serverSideEncryption(opts)...)

	return corev1.PodSpec{
		Containers: []corev1.Container{
			*container,
		},
		Volumes: volumes,
	}
}

func staticAuthCredentials(opts Options) []corev1.EnvVar {
	secretName := opts.SecretName
	switch opts.SharedStore {
	case lokiv1.ObjectStorageSecretAlibabaCloud:
		return []corev1.EnvVar{
			envVarFromSecret(EnvAlibabaCloudAccessKeyID, secretName, KeyAlibabaCloudAccessKeyID),
			envVarFromSecret(EnvAlibabaCloudAccessKeySecret, secretName, KeyAlibabaCloudSecretAccessKey),
		}
	case lokiv1.ObjectStorageSecretAzure:
		return []corev1.EnvVar{
			envVarFromSecret(EnvAzureStorageAccountName, secretName, KeyAzureStorageAccountName),
			envVarFromSecret(EnvAzureStorageAccountKey, secretName, KeyAzureStorageAccountKey),
		}
	case lokiv1.ObjectStorageSecretGCS:
		return []corev1.EnvVar{
			envVarFromValue(EnvGoogleApplicationCredentials, path.Join(secretDirectory, KeyGCPServiceAccountKeyFilename)),
		}
	case lokiv1.ObjectStorageSecretS3:
		return []corev1.EnvVar{
			envVarFromSecret(EnvAWSAccessKeyID, secretName, KeyAWSAccessKeyID),
			envVarFromSecret(EnvAWSAccessKeySecret, secretName, KeyAWSAccessKeySecret),
		}
	case lokiv1.ObjectStorageSecretSwift:
		return []corev1.EnvVar{
			envVarFromSecret(EnvSwiftUsername, secretName, KeySwiftUsername),
			envVarFromSecret(EnvSwiftPassword, secretName, KeySwiftPassword),
		}
	default:
		return []corev1.EnvVar{}
	}
}

func tokenAuthCredentials(opts Options) []corev1.EnvVar {
	switch opts.SharedStore {
	case lokiv1.ObjectStorageSecretS3:
		if opts.OpenShift.TokenCCOAuthEnabled() {
			return []corev1.EnvVar{
				envVarFromValue(EnvAWSCredentialsFile, path.Join(tokenAuthConfigDirectory, KeyAWSCredentialsFilename)),
				envVarFromValue(EnvAWSSdkLoadConfig, "true"),
			}
		} else {
			return []corev1.EnvVar{
				envVarFromSecret(EnvAWSRoleArn, opts.SecretName, KeyAWSRoleArn),
				envVarFromValue(EnvAWSWebIdentityTokenFile, ServiceAccountTokenFilePath),
			}
		}
	case lokiv1.ObjectStorageSecretAzure:
		if opts.OpenShift.TokenCCOAuthEnabled() {
			return []corev1.EnvVar{
				envVarFromSecret(EnvAzureStorageAccountName, opts.SecretName, KeyAzureStorageAccountName),
				envVarFromSecret(EnvAzureClientID, opts.OpenShift.CloudCredentials.SecretName, azureManagedCredentialKeyClientID),
				envVarFromSecret(EnvAzureTenantID, opts.OpenShift.CloudCredentials.SecretName, azureManagedCredentialKeyTenantID),
				envVarFromSecret(EnvAzureSubscriptionID, opts.OpenShift.CloudCredentials.SecretName, azureManagedCredentialKeySubscriptionID),
				envVarFromValue(EnvAzureFederatedTokenFile, ServiceAccountTokenFilePath),
			}
		}

		return []corev1.EnvVar{
			envVarFromSecret(EnvAzureStorageAccountName, opts.SecretName, KeyAzureStorageAccountName),
			envVarFromSecret(EnvAzureClientID, opts.SecretName, KeyAzureStorageClientID),
			envVarFromSecret(EnvAzureTenantID, opts.SecretName, KeyAzureStorageTenantID),
			envVarFromSecret(EnvAzureSubscriptionID, opts.SecretName, KeyAzureStorageSubscriptionID),
			envVarFromValue(EnvAzureFederatedTokenFile, ServiceAccountTokenFilePath),
		}
	case lokiv1.ObjectStorageSecretGCS:
		if opts.OpenShift.TokenCCOAuthEnabled() {
			return []corev1.EnvVar{
				envVarFromValue(EnvGoogleApplicationCredentials, path.Join(tokenAuthConfigDirectory, KeyGCPManagedServiceAccountKeyFilename)),
			}
		} else {
			return []corev1.EnvVar{
				envVarFromValue(EnvGoogleApplicationCredentials, path.Join(secretDirectory, KeyGCPServiceAccountKeyFilename)),
			}
		}
	default:
		return []corev1.EnvVar{}
	}
}

func serverSideEncryption(opts Options) []corev1.EnvVar {
	secretName := opts.SecretName
	switch opts.SharedStore {
	case lokiv1.ObjectStorageSecretS3:
		if opts.S3 != nil && opts.S3.SSE.Type == SSEKMSType && opts.S3.SSE.KMSEncryptionContext != "" {
			return []corev1.EnvVar{
				envVarFromSecret(EnvAWSSseKmsEncryptionContext, secretName, KeyAWSSseKmsEncryptionContext),
			}
		}
		return []corev1.EnvVar{}
	default:
		return []corev1.EnvVar{}
	}
}

func ensureCAForObjectStorage(p *corev1.PodSpec, tls *TLSConfig, secretType lokiv1.ObjectStorageSecretType) corev1.PodSpec {
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

	switch secretType {
	case lokiv1.ObjectStorageSecretS3:
		container.Args = append(container.Args,
			fmt.Sprintf("-s3.http.ca-file=%s", path.Join(caDirectory, tls.Key)),
		)
	case lokiv1.ObjectStorageSecretSwift:
		container.Args = append(container.Args,
			fmt.Sprintf("-swift.http.tls-ca-path=%s", path.Join(caDirectory, tls.Key)),
		)
	}

	return corev1.PodSpec{
		Containers: []corev1.Container{
			*container,
		},
		Volumes: volumes,
	}
}

func envVarFromSecret(name, secretName, secretKey string) corev1.EnvVar {
	return corev1.EnvVar{
		Name: name,
		ValueFrom: &corev1.EnvVarSource{
			SecretKeyRef: &corev1.SecretKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: secretName,
				},
				Key: secretKey,
			},
		},
	}
}

func envVarFromValue(name, value string) corev1.EnvVar {
	return corev1.EnvVar{
		Name:  name,
		Value: value,
	}
}

func tokenAuthEnabled(opts Options) bool {
	switch opts.CredentialMode {
	case lokiv1.CredentialModeToken, lokiv1.CredentialModeTokenCCO:
		return true
	case lokiv1.CredentialModeStatic:
		fallthrough
	default:
	}
	return false
}

func saTokenVolume(opts Options) corev1.Volume {
	var audience string
	storeType := opts.SharedStore
	switch storeType {
	case lokiv1.ObjectStorageSecretS3:
		audience = awsDefaultAudience
		if opts.S3.Audience != "" {
			audience = opts.S3.Audience
		}
	case lokiv1.ObjectStorageSecretAzure:
		audience = azureDefaultAudience
		if opts.Azure.Audience != "" {
			audience = opts.Azure.Audience
		}
	case lokiv1.ObjectStorageSecretGCS:
		audience = gcpDefaultAudience
		if opts.GCS.Audience != "" {
			audience = opts.GCS.Audience
		}
	}
	return corev1.Volume{
		Name: saTokenVolumeName,
		VolumeSource: corev1.VolumeSource{
			Projected: &corev1.ProjectedVolumeSource{
				Sources: []corev1.VolumeProjection{
					{
						ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
							ExpirationSeconds: ptr.To(saTokenExpiration),
							Path:              corev1.ServiceAccountTokenKey,
							Audience:          audience,
						},
					},
				},
			},
		},
	}
}

func tokenCCOAuthConfigVolume(opts Options) corev1.Volume {
	return corev1.Volume{
		Name: tokenAuthConfigVolumeName,
		VolumeSource: corev1.VolumeSource{
			Secret: &corev1.SecretVolumeSource{
				SecretName: opts.OpenShift.CloudCredentials.SecretName,
			},
		},
	}
}

package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
)

func TestConfigureDeploymentForStorageType(t *testing.T) {
	type tt struct {
		desc string
		opts Options
		dpl  *appsv1.Deployment
		want *appsv1.Deployment
	}

	tc := []tt{
		{
			desc: "object storage AlibabaCloud",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretAlibabaCloud,
			},
			dpl: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
									},
									Env: []corev1.EnvVar{
										{
											Name: EnvAlibabaCloudAccessKeyID,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAlibabaCloudAccessKeyID,
												},
											},
										},
										{
											Name: EnvAlibabaCloudAccessKeySecret,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAlibabaCloudSecretAccessKey,
												},
											},
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage Azure",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretAzure,
			},
			dpl: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
									},
									Env: []corev1.EnvVar{
										{
											Name: EnvAzureStorageAccountName,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAzureStorageAccountName,
												},
											},
										},
										{
											Name: EnvAzureStorageAccountKey,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAzureStorageAccountKey,
												},
											},
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage GCS",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretGCS,
			},
			dpl: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
									},
									Env: []corev1.EnvVar{
										{
											Name:  EnvGoogleApplicationCredentials,
											Value: "/etc/storage/secrets/key.json",
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage S3",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretS3,
			},
			dpl: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
									},
									Env: []corev1.EnvVar{
										{
											Name: EnvAWSAccessKeyID,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAWSAccessKeyID,
												},
											},
										},
										{
											Name: EnvAWSAccessKeySecret,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAWSAccessKeySecret,
												},
											},
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage S3 with SSE KMS encryption context",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretS3,
				S3: &S3StorageConfig{
					SSE: S3SSEConfig{
						Type:                 SSEKMSType,
						KMSEncryptionContext: "test",
					},
				},
			},
			dpl: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
									},
									Env: []corev1.EnvVar{
										{
											Name: EnvAWSAccessKeyID,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAWSAccessKeyID,
												},
											},
										},
										{
											Name: EnvAWSAccessKeySecret,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAWSAccessKeySecret,
												},
											},
										},
										{
											Name: EnvAWSSseKmsEncryptionContext,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAWSSseKmsEncryptionContext,
												},
											},
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage Swift",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretSwift,
			},
			dpl: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
									},
									Env: []corev1.EnvVar{
										{
											Name: EnvSwiftUsername,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeySwiftUsername,
												},
											},
										},
										{
											Name: EnvSwiftPassword,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeySwiftPassword,
												},
											},
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tc {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			err := ConfigureDeployment(tc.dpl, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.dpl)
		})
	}
}

func TestConfigureStatefulSetForStorageType(t *testing.T) {
	type tt struct {
		desc string
		opts Options
		sts  *appsv1.StatefulSet
		want *appsv1.StatefulSet
	}

	tc := []tt{
		{
			desc: "object storage AlibabaCloud",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretAlibabaCloud,
			},
			sts: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
								},
							},
						},
					},
				},
			},
			want: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
									},
									Env: []corev1.EnvVar{
										{
											Name: EnvAlibabaCloudAccessKeyID,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAlibabaCloudAccessKeyID,
												},
											},
										},
										{
											Name: EnvAlibabaCloudAccessKeySecret,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAlibabaCloudSecretAccessKey,
												},
											},
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage Azure",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretAzure,
			},
			sts: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
								},
							},
						},
					},
				},
			},
			want: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
									},
									Env: []corev1.EnvVar{
										{
											Name: EnvAzureStorageAccountName,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAzureStorageAccountName,
												},
											},
										},
										{
											Name: EnvAzureStorageAccountKey,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAzureStorageAccountKey,
												},
											},
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage GCS",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretGCS,
			},
			sts: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
								},
							},
						},
					},
				},
			},
			want: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
									},
									Env: []corev1.EnvVar{
										{
											Name:  EnvGoogleApplicationCredentials,
											Value: "/etc/storage/secrets/key.json",
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage S3",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretS3,
			},
			sts: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
								},
							},
						},
					},
				},
			},
			want: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
									},
									Env: []corev1.EnvVar{
										{
											Name: EnvAWSAccessKeyID,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAWSAccessKeyID,
												},
											},
										},
										{
											Name: EnvAWSAccessKeySecret,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAWSAccessKeySecret,
												},
											},
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage S3 with SSE KMS encryption Context",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretS3,
				S3: &S3StorageConfig{
					SSE: S3SSEConfig{
						Type:                 SSEKMSType,
						KMSEncryptionContext: "test",
					},
				},
			},
			sts: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
								},
							},
						},
					},
				},
			},
			want: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
									},
									Env: []corev1.EnvVar{
										{
											Name: EnvAWSAccessKeyID,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAWSAccessKeyID,
												},
											},
										},
										{
											Name: EnvAWSAccessKeySecret,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAWSAccessKeySecret,
												},
											},
										},
										{
											Name: EnvAWSSseKmsEncryptionContext,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAWSSseKmsEncryptionContext,
												},
											},
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage Swift",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretSwift,
			},
			sts: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
								},
							},
						},
					},
				},
			},
			want: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
									},
									Env: []corev1.EnvVar{
										{
											Name: EnvSwiftUsername,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeySwiftUsername,
												},
											},
										},
										{
											Name: EnvSwiftPassword,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeySwiftPassword,
												},
											},
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tc {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			err := ConfigureStatefulSet(tc.sts, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.sts)
		})
	}
}

func TestConfigureDeploymentForStorageCA(t *testing.T) {
	type tt struct {
		desc string
		opts Options
		dpl  *appsv1.Deployment
		want *appsv1.Deployment
	}

	tc := []tt{
		{
			desc: "object storage other than S3",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretSwift,
			},
			dpl: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-querier",
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-querier",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
									},
									Env: []corev1.EnvVar{
										{
											Name: EnvSwiftUsername,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeySwiftUsername,
												},
											},
										},
										{
											Name: EnvSwiftPassword,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeySwiftPassword,
												},
											},
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage S3",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretS3,
				TLS: &TLSConfig{
					CA:  "test",
					Key: "service-ca.crt",
				},
			},
			dpl: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-querier",
								},
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-querier",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
										{
											Name:      "storage-tls",
											ReadOnly:  false,
											MountPath: "/etc/storage/ca",
										},
									},
									Args: []string{
										"-s3.http.ca-file=/etc/storage/ca/service-ca.crt",
									},
									Env: []corev1.EnvVar{
										{
											Name: EnvAWSAccessKeyID,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAWSAccessKeyID,
												},
											},
										},
										{
											Name: EnvAWSAccessKeySecret,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAWSAccessKeySecret,
												},
											},
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
								{
									Name: "storage-tls",
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "test",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tc {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			err := ConfigureDeployment(tc.dpl, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.dpl)
		})
	}
}

func TestConfigureStatefulSetForStorageCA(t *testing.T) {
	type tt struct {
		desc string
		opts Options
		sts  *appsv1.StatefulSet
		want *appsv1.StatefulSet
	}

	tc := []tt{
		{
			desc: "object storage other than S3",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretSwift,
				TLS: &TLSConfig{
					CA: "test",
				},
			},
			sts: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
								},
							},
						},
					},
				},
			},
			want: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
									},
									Env: []corev1.EnvVar{
										{
											Name: EnvSwiftUsername,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeySwiftUsername,
												},
											},
										},
										{
											Name: EnvSwiftPassword,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeySwiftPassword,
												},
											},
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage S3",
			opts: Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretS3,
				TLS: &TLSConfig{
					CA:  "test",
					Key: "service-ca.crt",
				},
			},
			sts: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
								},
							},
						},
					},
				},
			},
			want: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: "/etc/storage/secrets",
										},
										{
											Name:      "storage-tls",
											ReadOnly:  false,
											MountPath: "/etc/storage/ca",
										},
									},
									Args: []string{
										"-s3.http.ca-file=/etc/storage/ca/service-ca.crt",
									},
									Env: []corev1.EnvVar{
										{
											Name: EnvAWSAccessKeyID,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAWSAccessKeyID,
												},
											},
										},
										{
											Name: EnvAWSAccessKeySecret,
											ValueFrom: &corev1.EnvVarSource{
												SecretKeyRef: &corev1.SecretKeySelector{
													LocalObjectReference: corev1.LocalObjectReference{
														Name: "test",
													},
													Key: KeyAWSAccessKeySecret,
												},
											},
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test",
									VolumeSource: corev1.VolumeSource{
										Secret: &corev1.SecretVolumeSource{
											SecretName: "test",
										},
									},
								},
								{
									Name: "storage-tls",
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "test",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tc {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			err := ConfigureStatefulSet(tc.sts, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.sts)
		})
	}
}

package storage_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

func TestConfigureDeploymentForStorageType(t *testing.T) {
	type tt struct {
		desc string
		opts storage.Options
		dpl  *appsv1.Deployment
		want *appsv1.Deployment
	}

	tc := []tt{
		{
			desc: "object storage other than GCS",
			opts: storage.Options{
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
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage GCS",
			opts: storage.Options{
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
											Name:  storage.EnvGoogleApplicationCredentials,
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
	}

	for _, tc := range tc {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			err := storage.ConfigureDeployment(tc.dpl, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.dpl)
		})
	}
}

func TestConfigureStatefulSetForStorageType(t *testing.T) {
	type tt struct {
		desc string
		opts storage.Options
		sts  *appsv1.StatefulSet
		want *appsv1.StatefulSet
	}

	tc := []tt{
		{
			desc: "object storage other than GCS",
			opts: storage.Options{
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
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage GCS",
			opts: storage.Options{
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
											Name:  storage.EnvGoogleApplicationCredentials,
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
	}

	for _, tc := range tc {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			err := storage.ConfigureStatefulSet(tc.sts, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.sts)
		})
	}
}

func TestConfigureDeploymentForStorageCA(t *testing.T) {
	type tt struct {
		desc string
		opts storage.Options
		dpl  *appsv1.Deployment
		want *appsv1.Deployment
	}

	tc := []tt{
		{
			desc: "object storage other than S3",
			opts: storage.Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretAzure,
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
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage S3",
			opts: storage.Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretS3,
				TLS: &storage.TLSConfig{
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
											Name:      "storage-tls",
											ReadOnly:  false,
											MountPath: "/etc/storage/ca",
										},
									},
									Args: []string{
										"-s3.http.ca-file=/etc/storage/ca/service-ca.crt",
									},
								},
							},
							Volumes: []corev1.Volume{
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
			err := storage.ConfigureDeployment(tc.dpl, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.dpl)
		})
	}
}

func TestConfigureStatefulSetForStorageCA(t *testing.T) {
	type tt struct {
		desc string
		opts storage.Options
		sts  *appsv1.StatefulSet
		want *appsv1.StatefulSet
	}

	tc := []tt{
		{
			desc: "object storage other than S3",
			opts: storage.Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretAzure,
				TLS: &storage.TLSConfig{
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
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "object storage S3",
			opts: storage.Options{
				SecretName:  "test",
				SharedStore: lokiv1.ObjectStorageSecretS3,
				TLS: &storage.TLSConfig{
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
											Name:      "storage-tls",
											ReadOnly:  false,
											MountPath: "/etc/storage/ca",
										},
									},
									Args: []string{
										"-s3.http.ca-file=/etc/storage/ca/service-ca.crt",
									},
								},
							},
							Volumes: []corev1.Volume{
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
			err := storage.ConfigureStatefulSet(tc.sts, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.sts)
		})
	}
}

package manifests

import (
	"testing"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	"github.com/grafana/loki/operator/internal/manifests/storage"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestConfigureDeploymentForStorageType(t *testing.T) {
	type tt struct {
		desc        string
		storageType lokiv1beta1.ObjectStorageSecretType
		secretName  string
		dpl         *appsv1.Deployment
		want        *appsv1.Deployment
	}

	tc := []tt{
		{
			desc:        "object storage other than GCS",
			storageType: lokiv1beta1.ObjectStorageSecretS3,
			secretName:  "test",
			dpl: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      configVolumeName,
											ReadOnly:  false,
											MountPath: config.LokiConfigMountDir,
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: configVolumeName,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											DefaultMode: &defaultConfigMapMode,
											LocalObjectReference: corev1.LocalObjectReference{
												Name: lokiConfigMapName("stack-name"),
											},
										},
									},
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
											Name:      configVolumeName,
											ReadOnly:  false,
											MountPath: config.LokiConfigMountDir,
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: configVolumeName,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											DefaultMode: &defaultConfigMapMode,
											LocalObjectReference: corev1.LocalObjectReference{
												Name: lokiConfigMapName("stack-name"),
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
		{
			desc:        "object storage GCS",
			storageType: lokiv1beta1.ObjectStorageSecretGCS,
			secretName:  "test",
			dpl: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      configVolumeName,
											ReadOnly:  false,
											MountPath: config.LokiConfigMountDir,
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: configVolumeName,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											DefaultMode: &defaultConfigMapMode,
											LocalObjectReference: corev1.LocalObjectReference{
												Name: lokiConfigMapName("stack-name"),
											},
										},
									},
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
											Name:      configVolumeName,
											ReadOnly:  false,
											MountPath: config.LokiConfigMountDir,
										},
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: secretDirectory,
										},
									},
									Env: []corev1.EnvVar{
										{
											Name:  storage.EnvGoogleApplicationCredentials,
											Value: "/etc/proxy/secrets/key.json",
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: configVolumeName,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											DefaultMode: &defaultConfigMapMode,
											LocalObjectReference: corev1.LocalObjectReference{
												Name: lokiConfigMapName("stack-name"),
											},
										},
									},
								},
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
			err := configureDeploymentForStorageType(tc.dpl, tc.storageType, tc.secretName)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.dpl)
		})
	}
}

func TestConfigureStatefulSetForStorageType(t *testing.T) {
	type tt struct {
		desc        string
		storageType lokiv1beta1.ObjectStorageSecretType
		secretName  string
		sts         *appsv1.StatefulSet
		want        *appsv1.StatefulSet
	}

	tc := []tt{
		{
			desc:        "object storage other than GCS",
			storageType: lokiv1beta1.ObjectStorageSecretS3,
			secretName:  "test",
			sts: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      configVolumeName,
											ReadOnly:  false,
											MountPath: config.LokiConfigMountDir,
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: configVolumeName,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											DefaultMode: &defaultConfigMapMode,
											LocalObjectReference: corev1.LocalObjectReference{
												Name: lokiConfigMapName("stack-name"),
											},
										},
									},
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
											Name:      configVolumeName,
											ReadOnly:  false,
											MountPath: config.LokiConfigMountDir,
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: configVolumeName,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											DefaultMode: &defaultConfigMapMode,
											LocalObjectReference: corev1.LocalObjectReference{
												Name: lokiConfigMapName("stack-name"),
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
		{
			desc:        "object storage GCS",
			storageType: lokiv1beta1.ObjectStorageSecretGCS,
			secretName:  "test",
			sts: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "loki-ingester",
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      configVolumeName,
											ReadOnly:  false,
											MountPath: config.LokiConfigMountDir,
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: configVolumeName,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											DefaultMode: &defaultConfigMapMode,
											LocalObjectReference: corev1.LocalObjectReference{
												Name: lokiConfigMapName("stack-name"),
											},
										},
									},
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
											Name:      configVolumeName,
											ReadOnly:  false,
											MountPath: config.LokiConfigMountDir,
										},
										{
											Name:      "test",
											ReadOnly:  false,
											MountPath: secretDirectory,
										},
									},
									Env: []corev1.EnvVar{
										{
											Name:  storage.EnvGoogleApplicationCredentials,
											Value: "/etc/proxy/secrets/key.json",
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: configVolumeName,
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											DefaultMode: &defaultConfigMapMode,
											LocalObjectReference: corev1.LocalObjectReference{
												Name: lokiConfigMapName("stack-name"),
											},
										},
									},
								},
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
			err := configureStatefulSetForStorageType(tc.sts, tc.storageType, tc.secretName)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.sts)
		})
	}
}

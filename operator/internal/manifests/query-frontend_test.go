package manifests

import (
	"testing"

	v1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestNewQueryFrontendDeployment_SelectorMatchesLabels(t *testing.T) {
	ss := NewQueryFrontendDeployment(Options{
		Name:      "abcd",
		Namespace: "efgh",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})
	l := ss.Spec.Template.GetObjectMeta().GetLabels()
	for key, value := range ss.Spec.Selector.MatchLabels {
		require.Contains(t, l, key)
		require.Equal(t, l[key], value)
	}
}

func TestNewQueryFrontendDeployment_HasTemplateConfigHashAnnotation(t *testing.T) {
	ss := NewQueryFrontendDeployment(Options{
		Name:       "abcd",
		Namespace:  "efgh",
		ConfigSHA1: "deadbeef",
		Stack: lokiv1.LokiStackSpec{
			Template: &lokiv1.LokiTemplateSpec{
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: 1,
				},
			},
		},
	})
	expected := "loki.grafana.com/config-hash"
	annotations := ss.Spec.Template.Annotations
	require.Contains(t, annotations, expected)
	require.Equal(t, annotations[expected], "deadbeef")
}

func TestConfigureQueryFrontendDeploymentForMode(t *testing.T) {
	type tt struct {
		desc string
		opts *Options
		dpl  *appsv1.Deployment
		want *appsv1.Deployment
	}

	tc := []tt{
		{
			desc: "static mode",
			opts: &Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
			},
			dpl:  &appsv1.Deployment{},
			want: &appsv1.Deployment{},
		},
		{
			desc: "dynamic mode",
			opts: &Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Dynamic,
					},
				},
			},
			dpl:  &appsv1.Deployment{},
			want: &appsv1.Deployment{},
		},
		{
			desc: "openshift-logging mode",
			opts: &Options{
				Name:      "test",
				Namespace: "test-ns",
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
				},
				Gates: v1.FeatureGates{
					ServiceMonitorTLSEndpoints: true,
				},
			},
			dpl: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Args: []string{
										"-target=query-frontend",
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
									Args: []string{
										"-target=query-frontend",
										"-frontend.tail-proxy-url=https://test-querier-http.test-ns.svc.cluster.local:3100",
										"-frontend.tail-tls-config.tls-ca-path=/var/run/ca/service-ca.crt",
									},
									VolumeMounts: []corev1.VolumeMount{
										{
											Name:      "test-ca-bundle",
											ReadOnly:  true,
											MountPath: "/var/run/ca",
										},
									},
								},
							},
							Volumes: []corev1.Volume{
								{
									Name: "test-ca-bundle",
									VolumeSource: corev1.VolumeSource{
										ConfigMap: &corev1.ConfigMapVolumeSource{
											DefaultMode: &defaultConfigMapMode,
											LocalObjectReference: corev1.LocalObjectReference{
												Name: "test-ca-bundle",
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
			err := configureQueryFrontendDeploymentForMode(tc.dpl, tc.opts.Stack.Tenants.Mode, tc.opts)
			require.NoError(t, err)
			require.Equal(t, tc.want, tc.dpl)
		})
	}
}

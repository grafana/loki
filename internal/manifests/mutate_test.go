package manifests_test

import (
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/ViaQ/loki-operator/internal/manifests"
	"github.com/stretchr/testify/require"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/pointer"
)

func TestGetMutateFunc_MutateObjectMeta(t *testing.T) {
	got := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test": "test",
			},
			Annotations: map[string]string{
				"test": "test",
			},
		},
	}

	want := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test": "test",
			},
			Annotations: map[string]string{
				"test": "test",
			},
		},
	}

	f := manifests.MutateFuncFor(got, want)
	err := f()
	require.NoError(t, err)

	// Partial mutation checks
	require.Exactly(t, got.Labels, want.Labels)
	require.Exactly(t, got.Annotations, want.Annotations)
}

func TestGetMutateFunc_ReturnErrOnNotSupportedType(t *testing.T) {
	got := &corev1.ServiceAccount{}
	want := &corev1.ServiceAccount{}
	f := manifests.MutateFuncFor(got, want)

	require.Error(t, f())
}

func TestGetMutateFunc_MutateConfigMap(t *testing.T) {
	got := &corev1.ConfigMap{
		Data:       map[string]string{"test": "remain"},
		BinaryData: map[string][]byte{},
	}

	want := &corev1.ConfigMap{
		Data:       map[string]string{"test": "test"},
		BinaryData: map[string][]byte{"btest": []byte("btestss")},
	}

	f := manifests.MutateFuncFor(got, want)
	err := f()
	require.NoError(t, err)

	// Ensure partial mutation applied
	require.Equal(t, got.Labels, want.Labels)
	require.Equal(t, got.Annotations, want.Annotations)
	require.Equal(t, got.BinaryData, got.BinaryData)

	// Ensure not mutated
	require.NotEqual(t, got.Data, want.Data)
}

func TestGetMutateFunc_MutateServiceSpec(t *testing.T) {
	got := &corev1.Service{
		Spec: corev1.ServiceSpec{
			ClusterIP:  "none",
			ClusterIPs: []string{"8.8.8.8"},
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       7777,
					TargetPort: intstr.FromString("8888"),
				},
			},
			Selector: map[string]string{
				"select": "that",
			},
		},
	}

	want := &corev1.Service{
		Spec: corev1.ServiceSpec{
			ClusterIP:  "none",
			ClusterIPs: []string{"8.8.8.8", "9.9.9.9"},
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       9999,
					TargetPort: intstr.FromString("1111"),
				},
			},
			Selector: map[string]string{
				"select": "that",
				"and":    "other",
			},
		},
	}

	f := manifests.MutateFuncFor(got, want)
	err := f()
	require.NoError(t, err)

	// Ensure partial mutation applied
	require.ElementsMatch(t, got.Spec.Ports, want.Spec.Ports)
	require.Exactly(t, got.Spec.Selector, want.Spec.Selector)

	// Ensure not mutated
	require.Equal(t, got.Spec.ClusterIP, "none")
	require.Exactly(t, got.Spec.ClusterIPs, []string{"8.8.8.8"})
}

func TestGeMutateFunc_MutateDeploymentSpec(t *testing.T) {
	type test struct {
		name string
		got  *appsv1.Deployment
		want *appsv1.Deployment
	}
	table := []test{
		{
			name: "initial creation",
			got: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
					Replicas: pointer.Int32Ptr(1),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test"},
							},
						},
					},
					Strategy: appsv1.DeploymentStrategy{
						Type: appsv1.RecreateDeploymentStrategyType,
					},
				},
			},
			want: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
							"and":  "another",
						},
					},
					Replicas: pointer.Int32Ptr(2),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "test",
									Args: []string{"--do-nothing"},
								},
							},
						},
					},
					Strategy: appsv1.DeploymentStrategy{
						Type: appsv1.RollingUpdateDeploymentStrategyType,
					},
				},
			},
		},
		{
			name: "update spec without selector",
			got: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
					Replicas: pointer.Int32Ptr(1),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test"},
							},
						},
					},
					Strategy: appsv1.DeploymentStrategy{
						Type: appsv1.RecreateDeploymentStrategyType,
					},
				},
			},
			want: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
							"and":  "another",
						},
					},
					Replicas: pointer.Int32Ptr(2),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "test",
									Args: []string{"--do-nothing"},
								},
							},
						},
					},
					Strategy: appsv1.DeploymentStrategy{
						Type: appsv1.RollingUpdateDeploymentStrategyType,
					},
				},
			},
		},
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()
			f := manifests.MutateFuncFor(tst.got, tst.want)
			err := f()
			require.NoError(t, err)

			// Ensure conditional mutation applied
			if tst.got.CreationTimestamp.IsZero() {
				require.Equal(t, tst.got.Spec.Selector, tst.want.Spec.Selector)
			} else {
				require.NotEqual(t, tst.got.Spec.Selector, tst.want.Spec.Selector)
			}

			// Ensure partial mutation applied
			require.Equal(t, tst.got.Spec.Replicas, tst.want.Spec.Replicas)
			require.Equal(t, tst.got.Spec.Template, tst.want.Spec.Template)
			require.Equal(t, tst.got.Spec.Strategy, tst.want.Spec.Strategy)
		})
	}
}

func TestGeMutateFunc_MutateStatefulSetSpec(t *testing.T) {
	type test struct {
		name string
		got  *appsv1.StatefulSet
		want *appsv1.StatefulSet
	}
	table := []test{
		{
			name: "initial creation",
			got: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					PodManagementPolicy: appsv1.ParallelPodManagement,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
					Replicas: pointer.Int32Ptr(1),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test"},
							},
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
						{
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
							},
						},
					},
				},
			},
			want: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					PodManagementPolicy: appsv1.OrderedReadyPodManagement,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
							"and":  "another",
						},
					},
					Replicas: pointer.Int32Ptr(2),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "test",
									Args: []string{"--do-nothing"},
								},
							},
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
						{
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
									corev1.ReadOnlyMany,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "update spec without selector",
			got: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
				Spec: appsv1.StatefulSetSpec{
					PodManagementPolicy: appsv1.ParallelPodManagement,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
					Replicas: pointer.Int32Ptr(1),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "test"},
							},
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
						{
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
								},
							},
						},
					},
				},
			},
			want: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
				Spec: appsv1.StatefulSetSpec{
					PodManagementPolicy: appsv1.OrderedReadyPodManagement,
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
							"and":  "another",
						},
					},
					Replicas: pointer.Int32Ptr(2),
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "test",
									Args: []string{"--do-nothing"},
								},
							},
						},
					},
					VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
						{
							Spec: corev1.PersistentVolumeClaimSpec{
								AccessModes: []corev1.PersistentVolumeAccessMode{
									corev1.ReadWriteOnce,
									corev1.ReadWriteMany,
								},
							},
						},
					},
				},
			},
		},
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()
			f := manifests.MutateFuncFor(tst.got, tst.want)
			err := f()
			require.NoError(t, err)

			// Ensure conditional mutation applied
			if tst.got.CreationTimestamp.IsZero() {
				require.Equal(t, tst.got.Spec.Selector, tst.want.Spec.Selector)
			} else {
				require.NotEqual(t, tst.got.Spec.Selector, tst.want.Spec.Selector)
			}

			// Ensure partial mutation applied
			require.Equal(t, tst.got.Spec.Replicas, tst.want.Spec.Replicas)
			require.Equal(t, tst.got.Spec.Template, tst.want.Spec.Template)
			require.Equal(t, tst.got.Spec.VolumeClaimTemplates, tst.got.Spec.VolumeClaimTemplates)
		})
	}
}

func TestGeMutateFunc_MutateServiceMonitorSpec(t *testing.T) {
	type test struct {
		name string
		got  *monitoringv1.ServiceMonitor
		want *monitoringv1.ServiceMonitor
	}
	table := []test{
		{
			name: "initial creation",
			got: &monitoringv1.ServiceMonitor{
				Spec: monitoringv1.ServiceMonitorSpec{
					JobLabel: "some-job",
					Endpoints: []monitoringv1.Endpoint{
						{
							Port:            "loki-test",
							Path:            "/some-path",
							Scheme:          "https",
							BearerTokenFile: manifests.BearerTokenFile,
							TLSConfig: &monitoringv1.TLSConfig{
								SafeTLSConfig: monitoringv1.SafeTLSConfig{
									ServerName: "loki-test.some-ns.svc.cluster.local",
								},
								CAFile: manifests.PrometheusCAFile,
							},
						},
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
					NamespaceSelector: monitoringv1.NamespaceSelector{
						MatchNames: []string{"some-ns"},
					},
				},
			},
			want: &monitoringv1.ServiceMonitor{
				Spec: monitoringv1.ServiceMonitorSpec{
					JobLabel: "some-job-new",
					Endpoints: []monitoringv1.Endpoint{
						{
							Port:            "loki-test",
							Path:            "/some-path",
							Scheme:          "https",
							BearerTokenFile: manifests.BearerTokenFile,
							TLSConfig: &monitoringv1.TLSConfig{
								SafeTLSConfig: monitoringv1.SafeTLSConfig{
									ServerName: "loki-test.some-ns.svc.cluster.local",
								},
								CAFile: manifests.PrometheusCAFile,
							},
						},
						{
							Port:            "loki-test",
							Path:            "/some-new-path",
							Scheme:          "https",
							BearerTokenFile: manifests.BearerTokenFile,
							TLSConfig: &monitoringv1.TLSConfig{
								SafeTLSConfig: monitoringv1.SafeTLSConfig{
									ServerName: "loki-test.some-ns.svc.cluster.local",
								},
								CAFile: manifests.PrometheusCAFile,
							},
						},
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
							"and":  "another",
						},
					},
					NamespaceSelector: monitoringv1.NamespaceSelector{
						MatchNames: []string{"some-ns-new"},
					},
				},
			},
		},
		{
			name: "update spec without selector",
			got: &monitoringv1.ServiceMonitor{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
				Spec: monitoringv1.ServiceMonitorSpec{
					JobLabel: "some-job",
					Endpoints: []monitoringv1.Endpoint{
						{
							Port:            "loki-test",
							Path:            "/some-path",
							Scheme:          "https",
							BearerTokenFile: manifests.BearerTokenFile,
							TLSConfig: &monitoringv1.TLSConfig{
								SafeTLSConfig: monitoringv1.SafeTLSConfig{
									ServerName: "loki-test.some-ns.svc.cluster.local",
								},
								CAFile: manifests.PrometheusCAFile,
							},
						},
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
						},
					},
					NamespaceSelector: monitoringv1.NamespaceSelector{
						MatchNames: []string{"some-ns"},
					},
				},
			},
			want: &monitoringv1.ServiceMonitor{
				ObjectMeta: metav1.ObjectMeta{CreationTimestamp: metav1.Now()},
				Spec: monitoringv1.ServiceMonitorSpec{
					JobLabel: "some-job-new",
					Endpoints: []monitoringv1.Endpoint{
						{
							Port:            "loki-test",
							Path:            "/some-path",
							Scheme:          "https",
							BearerTokenFile: manifests.BearerTokenFile,
							TLSConfig: &monitoringv1.TLSConfig{
								SafeTLSConfig: monitoringv1.SafeTLSConfig{
									ServerName: "loki-test.some-ns.svc.cluster.local",
								},
								CAFile: manifests.PrometheusCAFile,
							},
						},
						{
							Port:            "loki-test",
							Path:            "/some-new-path",
							Scheme:          "https",
							BearerTokenFile: manifests.BearerTokenFile,
							TLSConfig: &monitoringv1.TLSConfig{
								SafeTLSConfig: monitoringv1.SafeTLSConfig{
									ServerName: "loki-test.some-ns.svc.cluster.local",
								},
								CAFile: manifests.PrometheusCAFile,
							},
						},
					},
					Selector: metav1.LabelSelector{
						MatchLabels: map[string]string{
							"test": "test",
							"and":  "another",
						},
					},
					NamespaceSelector: monitoringv1.NamespaceSelector{
						MatchNames: []string{"some-ns-new"},
					},
				},
			},
		},
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()
			f := manifests.MutateFuncFor(tst.got, tst.want)
			err := f()
			require.NoError(t, err)

			// Ensure not mutated
			require.NotEqual(t, tst.got.Spec.JobLabel, tst.want.Spec.JobLabel)
			require.NotEqual(t, tst.got.Spec.Endpoints, tst.want.Spec.Endpoints)
			require.NotEqual(t, tst.got.Spec.NamespaceSelector, tst.want.Spec.NamespaceSelector)
		})
	}
}

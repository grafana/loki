package manifests_test

import (
	"testing"

	"github.com/grafana/loki/operator/internal/manifests"

	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
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
	got := &corev1.Endpoints{}
	want := &corev1.Endpoints{}
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
	require.Equal(t, got.Data, want.Data)
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

func TestGetMutateFunc_MutateServiceAccountObjectMeta(t *testing.T) {
	type test struct {
		name string
		got  *corev1.ServiceAccount
		want *corev1.ServiceAccount
	}
	table := []test{
		{
			name: "update object meta",
			got: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"test": "test",
					},
					Annotations: map[string]string{
						"test": "test",
					},
				},
			},
			want: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"test":  "test",
						"other": "label",
					},
					Annotations: map[string]string{
						"test":  "test",
						"other": "annotation",
					},
				},
			},
		},
		{
			name: "no update secrets",
			got: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"test": "test",
					},
					Annotations: map[string]string{
						"test": "test",
					},
				},
				Secrets: []corev1.ObjectReference{
					{Name: "secret-me"},
				},
			},
			want: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"test":  "test",
						"other": "label",
					},
					Annotations: map[string]string{
						"test":  "test",
						"other": "annotation",
					},
				},
				Secrets: []corev1.ObjectReference{
					{Name: "secret-me"},
					{Name: "another-secret"},
				},
			},
		},
		{
			name: "no update image pull secrets",
			got: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"test": "test",
					},
					Annotations: map[string]string{
						"test": "test",
					},
				},
				ImagePullSecrets: []corev1.LocalObjectReference{
					{Name: "secret-me"},
				},
			},
			want: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"test":  "test",
						"other": "label",
					},
					Annotations: map[string]string{
						"test":  "test",
						"other": "annotation",
					},
				},
				ImagePullSecrets: []corev1.LocalObjectReference{
					{Name: "secret-me"},
					{Name: "another-secret"},
				},
			},
		},
	}

	for _, tt := range table {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := manifests.MutateFuncFor(tt.got, tt.want)
			err := f()
			require.NoError(t, err)

			// Partial mutation checks
			require.Exactly(t, tt.got.Labels, tt.want.Labels)
			require.Exactly(t, tt.got.Annotations, tt.want.Annotations)

			if tt.got.Secrets != nil {
				require.NotEqual(t, tt.got.Secrets, tt.want.Secrets)
			}
			if tt.got.ImagePullSecrets != nil {
				require.NotEqual(t, tt.got.ImagePullSecrets, tt.want.ImagePullSecrets)
			}
		})
	}
}

func TestGetMutateFunc_MutateClusterRole(t *testing.T) {
	got := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test": "test",
			},
			Annotations: map[string]string{
				"test": "test",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"group-a"},
				Resources: []string{"res-a"},
				Verbs:     []string{"get"},
			},
		},
	}

	want := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test":  "test",
				"other": "label",
			},
			Annotations: map[string]string{
				"test":  "test",
				"other": "annotation",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"groupa-a"},
				Resources: []string{"resa-a"},
				Verbs:     []string{"get", "create"},
			},
			{
				APIGroups: []string{"groupa-b"},
				Resources: []string{"resa-b"},
				Verbs:     []string{"list", "create"},
			},
		},
	}

	f := manifests.MutateFuncFor(got, want)
	err := f()
	require.NoError(t, err)

	// Partial mutation checks
	require.Exactly(t, got.Labels, want.Labels)
	require.Exactly(t, got.Annotations, want.Annotations)
	require.Exactly(t, got.Rules, want.Rules)
}

func TestGetMutateFunc_MutateClusterRoleBinding(t *testing.T) {
	roleRef := rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "ClusterRole",
		Name:     "a-role",
	}

	got := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test": "test",
			},
			Annotations: map[string]string{
				"test": "test",
			},
		},
		RoleRef: roleRef,
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "service-me",
				Namespace: "stack-ns",
			},
		},
	}

	want := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test":  "test",
				"other": "label",
			},
			Annotations: map[string]string{
				"test":  "test",
				"other": "annotation",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "ClusterRole",
			Name:     "b-role",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "service-me",
				Namespace: "stack-ns",
			},
			{
				Kind: "User",
				Name: "a-user",
			},
		},
	}

	f := manifests.MutateFuncFor(got, want)
	err := f()
	require.NoError(t, err)

	// Partial mutation checks
	require.Exactly(t, got.Labels, want.Labels)
	require.Exactly(t, got.Annotations, want.Annotations)
	require.Exactly(t, got.RoleRef, roleRef)
	require.Exactly(t, got.Subjects, want.Subjects)
}

func TestGetMutateFunc_MutateRole(t *testing.T) {
	got := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test": "test",
			},
			Annotations: map[string]string{
				"test": "test",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"group-a"},
				Resources: []string{"res-a"},
				Verbs:     []string{"get"},
			},
		},
	}

	want := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test":  "test",
				"other": "label",
			},
			Annotations: map[string]string{
				"test":  "test",
				"other": "annotation",
			},
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{"groupa-a"},
				Resources: []string{"resa-a"},
				Verbs:     []string{"get", "create"},
			},
			{
				APIGroups: []string{"groupa-b"},
				Resources: []string{"resa-b"},
				Verbs:     []string{"list", "create"},
			},
		},
	}

	f := manifests.MutateFuncFor(got, want)
	err := f()
	require.NoError(t, err)

	// Partial mutation checks
	require.Exactly(t, got.Labels, want.Labels)
	require.Exactly(t, got.Annotations, want.Annotations)
	require.Exactly(t, got.Rules, want.Rules)
}

func TestGetMutateFunc_MutateRoleBinding(t *testing.T) {
	roleRef := rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "Role",
		Name:     "a-role",
	}

	got := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test": "test",
			},
			Annotations: map[string]string{
				"test": "test",
			},
		},
		RoleRef: roleRef,
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "service-me",
				Namespace: "stack-ns",
			},
		},
	}

	want := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test":  "test",
				"other": "label",
			},
			Annotations: map[string]string{
				"test":  "test",
				"other": "annotation",
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: "rbac.authorization.k8s.io",
			Kind:     "Role",
			Name:     "b-role",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      "service-me",
				Namespace: "stack-ns",
			},
			{
				Kind: "User",
				Name: "a-user",
			},
		},
	}

	f := manifests.MutateFuncFor(got, want)
	err := f()
	require.NoError(t, err)

	// Partial mutation checks
	require.Exactly(t, got.Labels, want.Labels)
	require.Exactly(t, got.Annotations, want.Annotations)
	require.Exactly(t, got.RoleRef, roleRef)
	require.Exactly(t, got.Subjects, want.Subjects)
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

func TestGetMutateFunc_MutateServiceMonitorSpec(t *testing.T) {
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

func TestGetMutateFunc_MutateIngress(t *testing.T) {
	pt := networkingv1.PathTypeExact
	got := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test": "test",
			},
			Annotations: map[string]string{
				"test": "test",
			},
		},
	}
	want := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test":  "test",
				"other": "label",
			},
			Annotations: map[string]string{
				"test":  "test",
				"other": "annotation",
			},
		},
		Spec: networkingv1.IngressSpec{
			DefaultBackend: &networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{
					Name: "a-service",
					Port: networkingv1.ServiceBackendPort{
						Name: "a-port",
					},
				},
			},
			Rules: []networkingv1.IngressRule{
				{
					Host: "a-host",
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									PathType: &pt,
									Path:     "/to/a/service",
								},
							},
						},
					},
				},
			},
			TLS: []networkingv1.IngressTLS{
				{
					Hosts:      []string{"a-host"},
					SecretName: "a-sercet",
				},
			},
		},
	}

	f := manifests.MutateFuncFor(got, want)
	err := f()
	require.NoError(t, err)

	// Partial mutation checks
	require.Exactly(t, got.Labels, want.Labels)
	require.Exactly(t, got.Annotations, want.Annotations)
	require.Exactly(t, got.Spec.DefaultBackend, want.Spec.DefaultBackend)
	require.Exactly(t, got.Spec.Rules, want.Spec.Rules)
	require.Exactly(t, got.Spec.TLS, want.Spec.TLS)
}

func TestGetMutateFunc_MutateRoute(t *testing.T) {
	got := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test": "test",
			},
			Annotations: map[string]string{
				"test": "test",
			},
		},
	}

	want := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test":  "test",
				"other": "label",
			},
			Annotations: map[string]string{
				"test":  "test",
				"other": "annotation",
			},
		},
		Spec: routev1.RouteSpec{
			Host: "a-host",
			To: routev1.RouteTargetReference{
				Kind:   "Service",
				Name:   "a-service",
				Weight: pointer.Int32(100),
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationReencrypt,
				Certificate:                   "a-cert",
				Key:                           "a-key",
				CACertificate:                 "a-ca-cert",
				DestinationCACertificate:      "a-dst-ca-cert",
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			},
		},
	}

	f := manifests.MutateFuncFor(got, want)
	err := f()
	require.NoError(t, err)

	// Partial mutation checks
	require.Exactly(t, got.Labels, want.Labels)
	require.Exactly(t, got.Annotations, want.Annotations)
	require.Exactly(t, got.Spec, want.Spec)
}

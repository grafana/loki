package manifests

import (
	"testing"

	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

func TestGetMutateFunc_MutateObjectMeta(t *testing.T) {
	want := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test": "test",
			},
			Annotations: map[string]string{
				"test": "test",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "loki.grafana.com/v1",
					BlockOwnerDeletion: ptr.To(true),
					Controller:         ptr.To(true),
					Kind:               "LokiStack",
					Name:               "lokistack-testing",
					UID:                "6128aa83-de7f-47c0-abf2-4a380713b599",
				},
			},
		},
	}

	got := &corev1.ConfigMap{}
	f := MutateFuncFor(got, want, nil)
	err := f()
	require.NoError(t, err)

	// Partial mutation checks
	require.Exactly(t, got.Labels, want.Labels)
	require.Exactly(t, got.Annotations, want.Annotations)
	require.Exactly(t, got.OwnerReferences, want.OwnerReferences)
}

func TestGetMutateFunc_ReturnErrOnNotSupportedType(t *testing.T) {
	got := &corev1.Endpoints{}
	want := &corev1.Endpoints{}
	f := MutateFuncFor(got, want, nil)

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

	f := MutateFuncFor(got, want, nil)
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

	f := MutateFuncFor(got, want, nil)
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
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := MutateFuncFor(tt.got, tt.want, nil)
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

	f := MutateFuncFor(got, want, nil)
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

	f := MutateFuncFor(got, want, nil)
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

	f := MutateFuncFor(got, want, nil)
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

	f := MutateFuncFor(got, want, nil)
	err := f()
	require.NoError(t, err)

	// Partial mutation checks
	require.Exactly(t, got.Labels, want.Labels)
	require.Exactly(t, got.Annotations, want.Annotations)
	require.Exactly(t, got.RoleRef, roleRef)
	require.Exactly(t, got.Subjects, want.Subjects)
}

func TestMutateFuncFor_MutateDeploymentSpec(t *testing.T) {
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
					Replicas: ptr.To[int32](1),
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
					Replicas: ptr.To[int32](2),
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
					Replicas: ptr.To[int32](1),
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
					Replicas: ptr.To[int32](2),
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
			name: "remove extra annotations and labels on pod",
			got: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"first-key":  "first-value",
								"second-key": "second-value",
							},
							Labels: map[string]string{
								"first-key":  "first-value",
								"second-key": "second-value",
							},
						},
					},
				},
			},
			want: &appsv1.Deployment{
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"first-key": "first-value",
							},
							Labels: map[string]string{
								"first-key": "first-value",
							},
						},
					},
				},
			},
		},
	}
	for _, tst := range table {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()
			f := MutateFuncFor(tst.got, tst.want, nil)
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

func TestMutateFuncFor_MutateStatefulSetSpec(t *testing.T) {
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
					Replicas: ptr.To[int32](1),
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
					Replicas: ptr.To[int32](2),
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
					Replicas: ptr.To[int32](1),
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
					Replicas: ptr.To[int32](2),
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
		{
			name: "remove extra annotations and labels on pod",
			got: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"first-key":  "first-value",
								"second-key": "second-value",
							},
							Labels: map[string]string{
								"first-key":  "first-value",
								"second-key": "second-value",
							},
						},
					},
				},
			},
			want: &appsv1.StatefulSet{
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								"first-key": "first-value",
							},
							Labels: map[string]string{
								"first-key": "first-value",
							},
						},
					},
				},
			},
		},
	}
	for _, tst := range table {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()
			f := MutateFuncFor(tst.got, tst.want, nil)
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
							BearerTokenFile: BearerTokenFile,
							TLSConfig: &monitoringv1.TLSConfig{
								SafeTLSConfig: monitoringv1.SafeTLSConfig{
									ServerName: ptr.To("loki-test.some-ns.svc.cluster.local"),
								},
								CAFile: PrometheusCAFile,
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
							BearerTokenFile: BearerTokenFile,
							TLSConfig: &monitoringv1.TLSConfig{
								SafeTLSConfig: monitoringv1.SafeTLSConfig{
									ServerName: ptr.To("loki-test.some-ns.svc.cluster.local"),
								},
								CAFile: PrometheusCAFile,
							},
						},
						{
							Port:            "loki-test",
							Path:            "/some-new-path",
							Scheme:          "https",
							BearerTokenFile: BearerTokenFile,
							TLSConfig: &monitoringv1.TLSConfig{
								SafeTLSConfig: monitoringv1.SafeTLSConfig{
									ServerName: ptr.To("loki-test.some-ns.svc.cluster.local"),
								},
								CAFile: PrometheusCAFile,
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
							BearerTokenFile: BearerTokenFile,
							TLSConfig: &monitoringv1.TLSConfig{
								SafeTLSConfig: monitoringv1.SafeTLSConfig{
									ServerName: ptr.To("loki-test.some-ns.svc.cluster.local"),
								},
								CAFile: PrometheusCAFile,
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
				ObjectMeta: metav1.ObjectMeta{
					CreationTimestamp: metav1.Now(),
					Labels:            map[string]string{"test": "label"},
					Annotations:       map[string]string{"test": "annotations"},
				},
				Spec: monitoringv1.ServiceMonitorSpec{
					JobLabel: "some-job-new",
					Endpoints: []monitoringv1.Endpoint{
						{
							Port:            "loki-test",
							Path:            "/some-path",
							Scheme:          "https",
							BearerTokenFile: BearerTokenFile,
							TLSConfig: &monitoringv1.TLSConfig{
								SafeTLSConfig: monitoringv1.SafeTLSConfig{
									ServerName: ptr.To("loki-test.some-ns.svc.cluster.local"),
								},
								CAFile: PrometheusCAFile,
							},
						},
						{
							Port:            "loki-test",
							Path:            "/some-new-path",
							Scheme:          "https",
							BearerTokenFile: BearerTokenFile,
							TLSConfig: &monitoringv1.TLSConfig{
								SafeTLSConfig: monitoringv1.SafeTLSConfig{
									ServerName: ptr.To("loki-test.some-ns.svc.cluster.local"),
								},
								CAFile: PrometheusCAFile,
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
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()
			f := MutateFuncFor(tst.got, tst.want, nil)
			err := f()
			require.NoError(t, err)

			// Ensure not mutated
			require.Equal(t, tst.got.Annotations, tst.want.Annotations)
			require.Equal(t, tst.got.Labels, tst.want.Labels)
			require.Equal(t, tst.got.Spec.Endpoints, tst.want.Spec.Endpoints)
			require.Equal(t, tst.got.Spec.JobLabel, tst.want.Spec.JobLabel)
			require.Equal(t, tst.got.Spec.Endpoints, tst.want.Spec.Endpoints)
			require.NotEqual(t, tst.got.Spec.NamespaceSelector, tst.want.Spec.NamespaceSelector)
			require.NotEqual(t, tst.got.Spec.Selector, tst.want.Spec.Selector)
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

	f := MutateFuncFor(got, want, nil)
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
				Weight: ptr.To[int32](100),
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
	f := MutateFuncFor(got, want, nil)

	err := f()
	require.NoError(t, err)

	// Partial mutation checks
	require.Exactly(t, got.Labels, want.Labels)
	require.Exactly(t, got.Annotations, want.Annotations)
	require.Exactly(t, got.Spec, want.Spec)
}

func TestGetMutateFunc_MutatePodDisruptionBudget(t *testing.T) {
	mu := intstr.FromInt(1)
	got := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"test":  "test",
				"other": "label",
			},
		},
	}

	want := &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"other": "label",
				"new":   "label",
			},
		},
		Spec: policyv1.PodDisruptionBudgetSpec{
			MinAvailable: &mu,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"label": "test",
					"and":   "another",
				},
			},
		},
	}

	f := MutateFuncFor(got, want, nil)
	err := f()

	require.NoError(t, err)
	require.Exactly(t, got.Labels, want.Labels)
	require.Exactly(t, got.Spec, want.Spec)
}

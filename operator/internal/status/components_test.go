package status

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/manifests"
)

func createPodList(baseName string, phases ...corev1.PodPhase) *corev1.PodList {
	items := []corev1.Pod{}
	for i, p := range phases {
		items = append(items, corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-pod-%d", baseName, i),
			},
			Status: corev1.PodStatus{
				Phase: p,
			},
		})
	}

	return &corev1.PodList{
		Items: items,
	}
}

func setupListClient(t *testing.T, stack *lokiv1.LokiStack, componentPods map[string]*corev1.PodList) (*k8sfakes.FakeClient, *k8sfakes.FakeStatusWriter) {
	k, sw := setupFakesNoError(t, stack)
	k.ListStub = func(_ context.Context, list client.ObjectList, options ...client.ListOption) error {
		componentLabel := ""
		for _, o := range options {
			if m, ok := o.(client.MatchingLabels); ok {
				componentLabel = m["app.kubernetes.io/component"]
			}
		}

		if componentLabel == "" {
			t.Fatalf("no component label on list call: %s", options)
		}

		podList, ok := componentPods[componentLabel]
		if !ok {
			t.Fatalf("no pods found for label: %s", componentLabel)
		}

		k.SetClientObjectList(list, podList)
		return nil
	}

	return k, sw
}

func TestGenerateComponentStatus(t *testing.T) {
	tt := []struct {
		desc                string
		componentPods       map[string]*corev1.PodList
		wantComponentStatus *lokiv1.LokiStackComponentStatus
	}{
		{
			desc: "no pods",
			componentPods: map[string]*corev1.PodList{
				manifests.LabelCompactorComponent:     {},
				manifests.LabelDistributorComponent:   {},
				manifests.LabelIngesterComponent:      {},
				manifests.LabelQuerierComponent:       {},
				manifests.LabelQueryFrontendComponent: {},
				manifests.LabelIndexGatewayComponent:  {},
				manifests.LabelRulerComponent:         {},
				manifests.LabelGatewayComponent:       {},
			},
			wantComponentStatus: &lokiv1.LokiStackComponentStatus{
				Compactor:     map[corev1.PodPhase][]string{},
				Distributor:   map[corev1.PodPhase][]string{},
				IndexGateway:  map[corev1.PodPhase][]string{},
				Ingester:      map[corev1.PodPhase][]string{},
				Querier:       map[corev1.PodPhase][]string{},
				QueryFrontend: map[corev1.PodPhase][]string{},
				Gateway:       map[corev1.PodPhase][]string{},
				Ruler:         map[corev1.PodPhase][]string{},
			},
		},
		{
			desc: "all one pod running",
			componentPods: map[string]*corev1.PodList{
				manifests.LabelCompactorComponent:     createPodList(manifests.LabelCompactorComponent, corev1.PodRunning),
				manifests.LabelDistributorComponent:   createPodList(manifests.LabelDistributorComponent, corev1.PodRunning),
				manifests.LabelIngesterComponent:      createPodList(manifests.LabelIngesterComponent, corev1.PodRunning),
				manifests.LabelQuerierComponent:       createPodList(manifests.LabelQuerierComponent, corev1.PodRunning),
				manifests.LabelQueryFrontendComponent: createPodList(manifests.LabelQueryFrontendComponent, corev1.PodRunning),
				manifests.LabelIndexGatewayComponent:  createPodList(manifests.LabelIndexGatewayComponent, corev1.PodRunning),
				manifests.LabelRulerComponent:         createPodList(manifests.LabelRulerComponent, corev1.PodRunning),
				manifests.LabelGatewayComponent:       createPodList(manifests.LabelGatewayComponent, corev1.PodRunning),
			},
			wantComponentStatus: &lokiv1.LokiStackComponentStatus{
				Compactor:     map[corev1.PodPhase][]string{corev1.PodRunning: {"compactor-pod-0"}},
				Distributor:   map[corev1.PodPhase][]string{corev1.PodRunning: {"distributor-pod-0"}},
				IndexGateway:  map[corev1.PodPhase][]string{corev1.PodRunning: {"index-gateway-pod-0"}},
				Ingester:      map[corev1.PodPhase][]string{corev1.PodRunning: {"ingester-pod-0"}},
				Querier:       map[corev1.PodPhase][]string{corev1.PodRunning: {"querier-pod-0"}},
				QueryFrontend: map[corev1.PodPhase][]string{corev1.PodRunning: {"query-frontend-pod-0"}},
				Gateway:       map[corev1.PodPhase][]string{corev1.PodRunning: {"lokistack-gateway-pod-0"}},
				Ruler:         map[corev1.PodPhase][]string{corev1.PodRunning: {"ruler-pod-0"}},
			},
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			stack := &lokiv1.LokiStack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
				},
			}

			k, _ := setupListClient(t, stack, tc.componentPods)

			componentStatus, err := generateComponentStatus(context.Background(), k, stack)
			require.NoError(t, err)
			require.Equal(t, tc.wantComponentStatus, componentStatus)

			// one list call for each component
			require.Equal(t, 8, k.ListCallCount())
		})
	}
}

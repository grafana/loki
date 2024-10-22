package status

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/manifests"
)

func createPodList(baseName string, ready bool, phases ...corev1.PodPhase) *corev1.PodList {
	items := []corev1.Pod{}
	for i, p := range phases {
		items = append(items, corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("%s-pod-%d", baseName, i),
			},
			Status: corev1.PodStatus{
				Phase: p,
				ContainerStatuses: []corev1.ContainerStatus{
					{
						Ready: ready,
					},
				},
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
	empty := lokiv1.PodStatusMap{
		lokiv1.PodFailed:  []string{},
		lokiv1.PodPending: []string{},
		lokiv1.PodRunning: []string{},
		lokiv1.PodReady:   []string{},
	}

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
				Compactor:     empty,
				Distributor:   empty,
				IndexGateway:  empty,
				Ingester:      empty,
				Querier:       empty,
				QueryFrontend: empty,
				Gateway:       empty,
				Ruler:         empty,
			},
		},
		{
			desc: "all one pod running",
			componentPods: map[string]*corev1.PodList{
				manifests.LabelCompactorComponent:     createPodList(manifests.LabelCompactorComponent, false, corev1.PodRunning),
				manifests.LabelDistributorComponent:   createPodList(manifests.LabelDistributorComponent, false, corev1.PodRunning),
				manifests.LabelIngesterComponent:      createPodList(manifests.LabelIngesterComponent, false, corev1.PodRunning),
				manifests.LabelQuerierComponent:       createPodList(manifests.LabelQuerierComponent, false, corev1.PodRunning),
				manifests.LabelQueryFrontendComponent: createPodList(manifests.LabelQueryFrontendComponent, false, corev1.PodRunning),
				manifests.LabelIndexGatewayComponent:  createPodList(manifests.LabelIndexGatewayComponent, false, corev1.PodRunning),
				manifests.LabelRulerComponent:         createPodList(manifests.LabelRulerComponent, false, corev1.PodRunning),
				manifests.LabelGatewayComponent:       createPodList(manifests.LabelGatewayComponent, false, corev1.PodRunning),
			},
			wantComponentStatus: &lokiv1.LokiStackComponentStatus{
				Compactor:     lokiv1.PodStatusMap{lokiv1.PodRunning: {"compactor-pod-0"}},
				Distributor:   lokiv1.PodStatusMap{lokiv1.PodRunning: {"distributor-pod-0"}},
				IndexGateway:  lokiv1.PodStatusMap{lokiv1.PodRunning: {"index-gateway-pod-0"}},
				Ingester:      lokiv1.PodStatusMap{lokiv1.PodRunning: {"ingester-pod-0"}},
				Querier:       lokiv1.PodStatusMap{lokiv1.PodRunning: {"querier-pod-0"}},
				QueryFrontend: lokiv1.PodStatusMap{lokiv1.PodRunning: {"query-frontend-pod-0"}},
				Gateway:       lokiv1.PodStatusMap{lokiv1.PodRunning: {"lokistack-gateway-pod-0"}},
				Ruler:         lokiv1.PodStatusMap{lokiv1.PodRunning: {"ruler-pod-0"}},
			},
		},
		{
			desc: "all pods without ruler",
			componentPods: map[string]*corev1.PodList{
				manifests.LabelCompactorComponent:     createPodList(manifests.LabelCompactorComponent, false, corev1.PodRunning),
				manifests.LabelDistributorComponent:   createPodList(manifests.LabelDistributorComponent, false, corev1.PodRunning),
				manifests.LabelIngesterComponent:      createPodList(manifests.LabelIngesterComponent, false, corev1.PodRunning),
				manifests.LabelQuerierComponent:       createPodList(manifests.LabelQuerierComponent, false, corev1.PodRunning),
				manifests.LabelQueryFrontendComponent: createPodList(manifests.LabelQueryFrontendComponent, false, corev1.PodRunning),
				manifests.LabelIndexGatewayComponent:  createPodList(manifests.LabelIndexGatewayComponent, false, corev1.PodRunning),
				manifests.LabelRulerComponent:         {},
				manifests.LabelGatewayComponent:       createPodList(manifests.LabelGatewayComponent, false, corev1.PodRunning),
			},
			wantComponentStatus: &lokiv1.LokiStackComponentStatus{
				Compactor:     lokiv1.PodStatusMap{lokiv1.PodRunning: {"compactor-pod-0"}},
				Distributor:   lokiv1.PodStatusMap{lokiv1.PodRunning: {"distributor-pod-0"}},
				IndexGateway:  lokiv1.PodStatusMap{lokiv1.PodRunning: {"index-gateway-pod-0"}},
				Ingester:      lokiv1.PodStatusMap{lokiv1.PodRunning: {"ingester-pod-0"}},
				Querier:       lokiv1.PodStatusMap{lokiv1.PodRunning: {"querier-pod-0"}},
				QueryFrontend: lokiv1.PodStatusMap{lokiv1.PodRunning: {"query-frontend-pod-0"}},
				Gateway:       lokiv1.PodStatusMap{lokiv1.PodRunning: {"lokistack-gateway-pod-0"}},
				Ruler:         empty,
			},
		},
	}

	for _, tc := range tt {
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

package status

import (
	"context"
	"testing"
	"time"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestRefreshSuccess(t *testing.T) {
	now := time.Now()
	stack := &lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	componentPods := map[string]*corev1.PodList{
		manifests.LabelCompactorComponent:     createPodList(manifests.LabelCompactorComponent, corev1.PodRunning),
		manifests.LabelDistributorComponent:   createPodList(manifests.LabelDistributorComponent, corev1.PodRunning),
		manifests.LabelIngesterComponent:      createPodList(manifests.LabelIngesterComponent, corev1.PodRunning),
		manifests.LabelQuerierComponent:       createPodList(manifests.LabelQuerierComponent, corev1.PodRunning),
		manifests.LabelQueryFrontendComponent: createPodList(manifests.LabelQueryFrontendComponent, corev1.PodRunning),
		manifests.LabelIndexGatewayComponent:  createPodList(manifests.LabelIndexGatewayComponent, corev1.PodRunning),
		manifests.LabelRulerComponent:         createPodList(manifests.LabelRulerComponent, corev1.PodRunning),
		manifests.LabelGatewayComponent:       createPodList(manifests.LabelGatewayComponent, corev1.PodRunning),
	}

	wantStatus := lokiv1.LokiStackStatus{
		Components: lokiv1.LokiStackComponentStatus{
			Compactor:     map[corev1.PodPhase][]string{corev1.PodRunning: {"compactor-pod-0"}},
			Distributor:   map[corev1.PodPhase][]string{corev1.PodRunning: {"distributor-pod-0"}},
			IndexGateway:  map[corev1.PodPhase][]string{corev1.PodRunning: {"index-gateway-pod-0"}},
			Ingester:      map[corev1.PodPhase][]string{corev1.PodRunning: {"ingester-pod-0"}},
			Querier:       map[corev1.PodPhase][]string{corev1.PodRunning: {"querier-pod-0"}},
			QueryFrontend: map[corev1.PodPhase][]string{corev1.PodRunning: {"query-frontend-pod-0"}},
			Gateway:       map[corev1.PodPhase][]string{corev1.PodRunning: {"lokistack-gateway-pod-0"}},
			Ruler:         map[corev1.PodPhase][]string{corev1.PodRunning: {"ruler-pod-0"}},
		},
		Storage: lokiv1.LokiStackStorageStatus{},
		Conditions: []metav1.Condition{
			{
				Type:               string(lokiv1.ConditionReady),
				Reason:             string(lokiv1.ReasonReadyComponents),
				Message:            messageReady,
				Status:             metav1.ConditionTrue,
				LastTransitionTime: metav1.NewTime(now),
			},
		},
	}

	k, sw := setupListClient(t, stack, componentPods)

	err := Refresh(context.Background(), k, req, now)

	require.NoError(t, err)
	require.Equal(t, 1, k.GetCallCount())
	require.Equal(t, 8, k.ListCallCount())

	require.Equal(t, 1, sw.UpdateCallCount())
	_, updated, _ := sw.UpdateArgsForCall(0)
	updatedStack, ok := updated.(*lokiv1.LokiStack)
	if !ok {
		t.Fatalf("not a LokiStack: %T", updatedStack)
	}

	require.Equal(t, wantStatus, updatedStack.Status)
}

func TestRefreshSuccess_ZoneAwarePendingPod(t *testing.T) {
	now := time.Now()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "test-ns",
		},
	}
	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-stack",
			Namespace: "test-ns",
		},
		Spec: lokiv1.LokiStackSpec{
			Replication: &lokiv1.ReplicationSpec{
				Zones: []lokiv1.ZoneSpec{
					{
						TopologyKey: corev1.LabelZoneFailureDomain,
					},
				},
			},
		},
	}
	testPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "test-ns",
			Labels: map[string]string{
				lokiv1.LabelZoneAwarePod: "enabled",
			},
			Annotations: map[string]string{
				lokiv1.AnnotationAvailabilityZoneLabels: corev1.LabelHostname + "," + corev1.LabelTopologyZone,
				corev1.LabelTopologyZone:                "us-east-2c",
			},
		},
		Status: corev1.PodStatus{
			Phase: v1.PodPending,
		},
	}

	k, sw := setupFakesNoError(t, &stack)

	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if name.Name == req.Name {
			k.SetClientObject(object, &stack)
			return nil
		}
		if name.Name == testPod.Name {
			k.SetClientObject(object, &testPod)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}
	k.ListStub = func(ctx context.Context, ol client.ObjectList, _ ...client.ListOption) error {
		switch ol.(type) {
		case *corev1.PodList:
			k.SetClientObjectList(ol, &corev1.PodList{
				Items: []corev1.Pod{
					testPod,
				},
			})
		case *corev1.NodeList:
			k.SetClientObjectList(ol, &corev1.NodeList{
				Items: []v1.Node{},
			})
		}
		return nil
	}

	err := Refresh(context.Background(), k, req, now)

	require.NoError(t, err)
	require.Equal(t, 1, k.GetCallCount())
	require.Equal(t, 9, k.ListCallCount())
	require.Equal(t, 1, sw.UpdateCallCount())
	_, updated, _ := sw.UpdateArgsForCall(0)
	updatedStack, ok := updated.(*lokiv1.LokiStack)
	if !ok {
		t.Fatalf("not a LokiStack: %T", updatedStack)
	}

	require.Contains(t, updatedStack.Status.Conditions[0].Reason, string(lokiv1.ReasonAvailabilityZoneLabelsMismatch))

}

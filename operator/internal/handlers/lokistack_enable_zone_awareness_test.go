package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
)

var defaultPod = corev1.Pod{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-pod",
		Namespace: "some-ns",
		Labels: map[string]string{
			lokiv1.LabelZoneAwarePod: "enabled",
		},
		Annotations: map[string]string{
			lokiv1.AnnotationAvailabilityZoneLabels: corev1.LabelTopologyZone,
			lokiv1.AnnotationAvailabilityZone:       "us-east-zone-2c",
		},
	},
	Spec: corev1.PodSpec{
		NodeName: "test-node",
	},
}

func TestAnnotatePodWithAvailabilityZone_WhenGetReturnsAnErrorOtherThanNotFound_ReturnsTheError(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	badRequestErr := kverrors.New("failed to lookup node")
	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		return badRequestErr
	}

	err := AnnotatePodWithAvailabilityZone(context.TODO(), logger, k, &defaultPod)
	require.Equal(t, badRequestErr, errors.Unwrap(err))

	// make sure patch was NOT called because the Get failed
	require.Zero(t, k.PatchCallCount())
}

func TestAnnotatePodWithAvailabilityZone_WhenGetReturnsNode_DoesNotError(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	testPod := corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "some-ns",
			Labels: map[string]string{
				lokiv1.LabelZoneAwarePod: "enabled",
			},
			Annotations: map[string]string{
				lokiv1.AnnotationAvailabilityZoneLabels: corev1.LabelHostname + "," + corev1.LabelTopologyZone + "," + corev1.LabelTopologyRegion,
			},
		},
		Spec: corev1.PodSpec{
			NodeName: "test-node",
		},
	}

	testNode := corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-node",
			Namespace: "some-ns",
			Labels: map[string]string{
				corev1.LabelHostname:       "test-node",
				corev1.LabelTopologyZone:   "us-east-2c",
				corev1.LabelTopologyRegion: "us-east-2",
			},
		},
	}

	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if name.Name == testPod.Spec.NodeName {
			k.SetClientObject(object, &testNode)
			return nil
		}
		return kverrors.New("failed to lookup node")
	}

	expectedPatch, _ := json.Marshal(map[string]interface{}{
		"metadata": map[string]interface{}{
			"annotations": map[string]string{
				lokiv1.AnnotationAvailabilityZone: "test-node_us-east-2c_us-east-2",
			},
		},
	})

	err := AnnotatePodWithAvailabilityZone(context.TODO(), logger, k, &testPod)
	require.NoError(t, err)

	// make sure patch was called because the Get succeeded
	require.Equal(t, 1, k.PatchCallCount())
	_, p, patch, _ := k.PatchArgsForCall(0)
	require.Equal(t, p, &testPod)
	actualPatch, _ := patch.Data(nil)
	require.Equal(t, actualPatch, expectedPatch)
}

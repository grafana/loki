package handlers_test

import (
	"context"
	"errors"
	"testing"

	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/handlers"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	// apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	// "k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	// ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	defaultZones = []lokiv1.ZoneSpec{
		{
			TopologyKey: corev1.LabelTopologyZone,
		},
	}

	defaultPod = corev1.Pod{
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

	defaultNode = corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-node",
			Namespace: "some-ns",
			Labels: map[string]string{
				corev1.LabelTopologyZone: "us-east-zone-2c",
			},
		},
	}
)

func TestAnnotatePodWithAvailabilityZone_WhenGetReturnsAnErrorOtherThanNotFound_ReturnsTheError(t *testing.T) {

	k := &k8sfakes.FakeClient{}

	badRequestErr := kverrors.New("failed to lookup node")
	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		return badRequestErr
	}

	err := handlers.AnnotatePodWithAvailabilityZone(context.TODO(), logger, k, &defaultPod)
	require.Equal(t, badRequestErr, errors.Unwrap(err))

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
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
				lokiv1.AnnotationAvailabilityZone:       "test-node_us-east-2c_us-east-2",
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
		k.SetClientObject(object, &testNode)
		return nil
	}

	err := handlers.AnnotatePodWithAvailabilityZone(context.TODO(), logger, k, &testPod)
	require.NoError(t, err)

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
}

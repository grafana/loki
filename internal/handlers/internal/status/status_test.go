package status_test

import (
	"context"
	"testing"

	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/external/k8s/k8sfakes"
	"github.com/ViaQ/loki-operator/internal/handlers/internal/status"

	"github.com/stretchr/testify/require"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSetDegradedCondition_WhenExisting_DoNothing(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	msg := "tell me nothing"
	reason := lokiv1beta1.ReasonMissingObjectStorageSecret
	s := lokiv1beta1.LokiStack{
		Status: lokiv1beta1.LokiStackStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(lokiv1beta1.ConditionDegraded),
					Reason: string(reason),
				},
			},
		},
	}

	err := status.SetDegradedCondition(context.TODO(), k, &s, msg, reason)
	require.NoError(t, err)
}

func TestSetDegradedCondition_WhenNoneExisting_AppendDegradedCondition(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	k.StatusStub = func() client.StatusWriter { return sw }

	msg := "tell me something"
	reason := lokiv1beta1.ReasonMissingObjectStorageSecret
	s := lokiv1beta1.LokiStack{}

	err := status.SetDegradedCondition(context.TODO(), k, &s, msg, reason)
	require.NoError(t, err)

	require.NotZero(t, k.StatusCallCount())
	require.NotZero(t, sw.UpdateCallCount())
	require.NotEmpty(t, s.Status.Conditions)
}

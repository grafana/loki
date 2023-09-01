package status

import (
	"context"
	"fmt"

	"github.com/ViaQ/logerr/v2/kverrors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
)

const (
	messageReady                      = "All components ready"
	messageFailed                     = "Some LokiStack components failed"
	messagePending                    = "Some LokiStack components pending on dependencies"
	messageDegradedNodeLabels         = "Cluster contains no nodes matching the labels used for zone-awareness"
	messageTopologyKeyNodeLabelsEmpty = "No value for the labels used for zone-awareness"
)

var (
	conditionFailed = metav1.Condition{
		Type:    string(lokiv1.ConditionFailed),
		Message: messageFailed,
		Reason:  string(lokiv1.ReasonFailedComponents),
	}
	conditionPending = metav1.Condition{
		Type:    string(lokiv1.ConditionPending),
		Message: messagePending,
		Reason:  string(lokiv1.ReasonPendingComponents),
	}
	conditionReady = metav1.Condition{
		Type:    string(lokiv1.ConditionReady),
		Message: messageReady,
		Reason:  string(lokiv1.ReasonReadyComponents),
	}
	conditionDegradedNodeLabels = metav1.Condition{
		Type:    string(lokiv1.ConditionDegraded),
		Message: messageDegradedNodeLabels,
		Reason:  string(lokiv1.ReasonNoZoneAwareNodes),
	}
	conditionTopologyKeyNodeLabelsEmpty = metav1.Condition{
		Type:    string(lokiv1.ConditionDegraded),
		Message: messageTopologyKeyNodeLabelsEmpty,
		Reason:  string(lokiv1.ReasonTopologyKeyNodeLabelsEmpty),
	}
)

// DegradedError contains information about why the managed LokiStack has an invalid configuration.
type DegradedError struct {
	Message string
	Reason  lokiv1.LokiStackConditionReason
	Requeue bool
}

func (e *DegradedError) Error() string {
	return fmt.Sprintf("cluster degraded: %s", e.Message)
}

// SetDegradedCondition appends the condition Degraded to the lokistack status conditions.
func SetDegradedCondition(ctx context.Context, k k8s.Client, req ctrl.Request, msg string, reason lokiv1.LokiStackConditionReason) error {
	degraded := metav1.Condition{
		Type:    string(lokiv1.ConditionDegraded),
		Message: msg,
		Reason:  string(reason),
	}

	return updateCondition(ctx, k, req, degraded)
}

func generateCondition(ctx context.Context, cs *lokiv1.LokiStackComponentStatus, k client.Client, req ctrl.Request, stack *lokiv1.LokiStack) (metav1.Condition, error) {
	// Check for failed pods first
	failed := len(cs.Compactor[corev1.PodFailed]) +
		len(cs.Distributor[corev1.PodFailed]) +
		len(cs.Ingester[corev1.PodFailed]) +
		len(cs.Querier[corev1.PodFailed]) +
		len(cs.QueryFrontend[corev1.PodFailed]) +
		len(cs.Gateway[corev1.PodFailed]) +
		len(cs.IndexGateway[corev1.PodFailed]) +
		len(cs.Ruler[corev1.PodFailed])

	if failed != 0 {
		return conditionFailed, nil
	}

	// Check for pending pods
	pending := len(cs.Compactor[corev1.PodPending]) +
		len(cs.Distributor[corev1.PodPending]) +
		len(cs.Ingester[corev1.PodPending]) +
		len(cs.Querier[corev1.PodPending]) +
		len(cs.QueryFrontend[corev1.PodPending]) +
		len(cs.Gateway[corev1.PodPending]) +
		len(cs.IndexGateway[corev1.PodPending]) +
		len(cs.Ruler[corev1.PodPending])

	if pending != 0 {
		if stack.Spec.Replication != nil && len(stack.Spec.Replication.Zones) > 0 {
			// When there are pending pods and zone-awareness is enabled check if there are any nodes
			// that can satisfy the constraints and emit a condition if not.
			condition, err := checkForZoneawareNodes(ctx, k, stack.Spec.Replication.Zones)
			if err != nil {
				return metav1.Condition{}, err
			}

			if condition != nil {
				return *condition, nil
			}
		}

		return conditionPending, nil
	}

	return conditionReady, nil
}

func checkForZoneawareNodes(ctx context.Context, k client.Client, zones []lokiv1.ZoneSpec) (*metav1.Condition, error) {
	nodeLabels := client.HasLabels{}
	for _, z := range zones {
		nodeLabels = append(nodeLabels, z.TopologyKey)
	}

	nodeList := &corev1.NodeList{}
	if err := k.List(ctx, nodeList, nodeLabels); err != nil {
		return nil, err
	}

	for _, node := range nodeList.Items {
		for _, nodeLabel := range nodeLabels {
			if node.Labels[nodeLabel] == "" {
				return &conditionTopologyKeyNodeLabelsEmpty, nil
			}
		}
	}

	return &conditionDegradedNodeLabels, nil
}

func updateCondition(ctx context.Context, k k8s.Client, req ctrl.Request, condition metav1.Condition) error {
	var stack lokiv1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &stack); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup LokiStack", "name", req.NamespacedName)
	}

	for _, c := range stack.Status.Conditions {
		if c.Type == condition.Type &&
			c.Reason == condition.Reason &&
			c.Message == condition.Message &&
			c.Status == metav1.ConditionTrue {
			// resource already has desired condition
			return nil
		}
	}

	condition.Status = metav1.ConditionTrue

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := k.Get(ctx, req.NamespacedName, &stack); err != nil {
			return err
		}

		now := metav1.Now()
		condition.LastTransitionTime = now

		index := -1
		for i := range stack.Status.Conditions {
			// Reset all other conditions first
			stack.Status.Conditions[i].Status = metav1.ConditionFalse
			stack.Status.Conditions[i].LastTransitionTime = now

			// Locate existing pending condition if any
			if stack.Status.Conditions[i].Type == condition.Type {
				index = i
			}
		}

		if index == -1 {
			stack.Status.Conditions = append(stack.Status.Conditions, condition)
		} else {
			stack.Status.Conditions[index] = condition
		}

		return k.Status().Update(ctx, &stack)
	})
}

package status

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"
)

// generateComponentStatus updates the pod status map component
func generateComponentStatus(ctx context.Context, k k8s.Client, s *lokiv1.LokiStack) (*lokiv1.LokiStackComponentStatus, error) {
	var err error
	result := &lokiv1.LokiStackComponentStatus{}
	result.Compactor, err = appendPodStatus(ctx, k, manifests.LabelCompactorComponent, s.Name, s.Namespace)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelCompactorComponent)
	}

	result.Querier, err = appendPodStatus(ctx, k, manifests.LabelQuerierComponent, s.Name, s.Namespace)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelQuerierComponent)
	}

	result.Distributor, err = appendPodStatus(ctx, k, manifests.LabelDistributorComponent, s.Name, s.Namespace)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelDistributorComponent)
	}

	result.QueryFrontend, err = appendPodStatus(ctx, k, manifests.LabelQueryFrontendComponent, s.Name, s.Namespace)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelQueryFrontendComponent)
	}

	result.IndexGateway, err = appendPodStatus(ctx, k, manifests.LabelIndexGatewayComponent, s.Name, s.Namespace)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelIngesterComponent)
	}

	result.Ingester, err = appendPodStatus(ctx, k, manifests.LabelIngesterComponent, s.Name, s.Namespace)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelIndexGatewayComponent)
	}

	result.Gateway, err = appendPodStatus(ctx, k, manifests.LabelGatewayComponent, s.Name, s.Namespace)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelGatewayComponent)
	}

	result.Ruler, err = appendPodStatus(ctx, k, manifests.LabelRulerComponent, s.Name, s.Namespace)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelRulerComponent)
	}

	return result, nil
}

func appendPodStatus(ctx context.Context, k k8s.Client, component, stack, ns string) (lokiv1.PodStatusMap, error) {
	psm := lokiv1.PodStatusMap{}
	pods := &corev1.PodList{}
	opts := []client.ListOption{
		client.MatchingLabels(manifests.ComponentLabels(component, stack)),
		client.InNamespace(ns),
	}
	if err := k.List(ctx, pods, opts...); err != nil {
		return nil, kverrors.Wrap(err, "failed to list pods for LokiStack component", "name", stack, "component", component)
	}
	for _, pod := range pods.Items {
		status := podStatus(&pod)
		psm[status] = append(psm[status], pod.Name)
	}

	if len(psm) == 0 {
		psm = lokiv1.PodStatusMap{
			lokiv1.PodFailed:  []string{},
			lokiv1.PodPending: []string{},
			lokiv1.PodRunning: []string{},
			lokiv1.PodReady:   []string{},
		}
	}
	return psm, nil
}

func podStatus(pod *corev1.Pod) lokiv1.PodStatus {
	status := pod.Status
	switch status.Phase {
	case corev1.PodFailed:
		return lokiv1.PodFailed
	case corev1.PodPending:
		return lokiv1.PodPending
	case corev1.PodRunning:
	default:
		return lokiv1.PodStatusUnknown
	}

	for _, c := range status.ContainerStatuses {
		if !c.Ready {
			return lokiv1.PodRunning
		}
	}

	return lokiv1.PodReady
}

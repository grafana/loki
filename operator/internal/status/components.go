package status

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		phase := pod.Status.Phase
		psm[phase] = append(psm[phase], pod.Name)
	}
	return psm, nil
}

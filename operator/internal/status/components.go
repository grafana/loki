package status

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SetComponentsStatus updates the pod status map component
func SetComponentsStatus(ctx context.Context, k k8s.Client, req ctrl.Request) error {
	var s lokiv1beta1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &s); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return kverrors.Wrap(err, "failed to lookup lokistack", "name", req.NamespacedName)
	}

	var err error
	s.Status.Components = lokiv1beta1.LokiStackComponentStatus{}
	s.Status.Components.Compactor, err = appendPodStatus(ctx, k, manifests.LabelCompactorComponent, s.Name, s.Namespace)
	if err != nil {
		return kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelCompactorComponent)
	}

	s.Status.Components.Querier, err = appendPodStatus(ctx, k, manifests.LabelQuerierComponent, s.Name, s.Namespace)
	if err != nil {
		return kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelQuerierComponent)
	}

	s.Status.Components.Distributor, err = appendPodStatus(ctx, k, manifests.LabelDistributorComponent, s.Name, s.Namespace)
	if err != nil {
		return kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelDistributorComponent)
	}

	s.Status.Components.QueryFrontend, err = appendPodStatus(ctx, k, manifests.LabelQueryFrontendComponent, s.Name, s.Namespace)
	if err != nil {
		return kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelQueryFrontendComponent)
	}

	s.Status.Components.IndexGateway, err = appendPodStatus(ctx, k, manifests.LabelIndexGatewayComponent, s.Name, s.Namespace)
	if err != nil {
		return kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelIngesterComponent)
	}

	s.Status.Components.Ingester, err = appendPodStatus(ctx, k, manifests.LabelIngesterComponent, s.Name, s.Namespace)
	if err != nil {
		return kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelIndexGatewayComponent)
	}

	s.Status.Components.Gateway, err = appendPodStatus(ctx, k, manifests.LabelGatewayComponent, s.Name, s.Namespace)
	if err != nil {
		return kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelGatewayComponent)
	}

	s.Status.Components.Ruler, err = appendPodStatus(ctx, k, manifests.LabelRulerComponent, s.Name, s.Namespace)
	if err != nil {
		return kverrors.Wrap(err, "failed lookup LokiStack component pods status", "name", manifests.LabelRulerComponent)
	}

	return k.Status().Update(ctx, &s, &client.UpdateOptions{})
}

func appendPodStatus(ctx context.Context, k k8s.Client, component, stack, ns string) (lokiv1beta1.PodStatusMap, error) {
	psm := lokiv1beta1.PodStatusMap{}
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

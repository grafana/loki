package rules

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/handlers/internal/openshift"
	"github.com/grafana/loki/operator/internal/manifests"
	manifestsocp "github.com/grafana/loki/operator/internal/manifests/openshift"
	"github.com/grafana/loki/operator/internal/status"
)

// BuildOptions returns the ruler options needed to generate Kubernetes resource manifests.
// The returned error can be a status.DegradedError in the following cases:
//   - When remote write is enabled and the authorization Secret is missing.
//   - When remote write is enabled and the authorization Secret data is invalid.
func BuildOptions(
	ctx context.Context,
	log logr.Logger,
	k k8s.Client,
	stack *lokiv1.LokiStack,
) ([]lokiv1.AlertingRule, []lokiv1.RecordingRule, manifests.Ruler, manifestsocp.Options, error) {
	if stack.Spec.Rules == nil || !stack.Spec.Rules.Enabled {
		return nil, nil, manifests.Ruler{}, manifestsocp.Options{}, nil
	}

	var (
		err            error
		alertingRules  []lokiv1.AlertingRule
		recordingRules []lokiv1.RecordingRule
		rulerConfig    *lokiv1.RulerConfigSpec
		rulerSecret    *manifests.RulerSecret
		ruler          manifests.Ruler
		ocpOpts        manifestsocp.Options

		stackKey = client.ObjectKeyFromObject(stack)
	)

	alertingRules, recordingRules, err = listRules(ctx, k, stack.Namespace, stack.Spec.Rules)
	if err != nil {
		log.Error(err, "failed to lookup rules", "spec", stack.Spec.Rules)
	}

	rulerConfig, err = getRulerConfig(ctx, k, stackKey)
	if err != nil {
		log.Error(err, "failed to lookup ruler config", "key", stackKey)
	}

	if rulerConfig != nil && rulerConfig.RemoteWriteSpec != nil && rulerConfig.RemoteWriteSpec.ClientSpec != nil {
		var rs corev1.Secret
		key := client.ObjectKey{Name: rulerConfig.RemoteWriteSpec.ClientSpec.AuthorizationSecretName, Namespace: stack.Namespace}
		if err = k.Get(ctx, key, &rs); err != nil {
			if apierrors.IsNotFound(err) {
				return nil, nil, ruler, ocpOpts, &status.DegradedError{
					Message: "Missing ruler remote write authorization secret",
					Reason:  lokiv1.ReasonMissingRulerSecret,
					Requeue: false,
				}
			}
			return nil, nil, ruler, ocpOpts, kverrors.Wrap(err, "failed to lookup lokistack ruler secret", "name", key)
		}

		rulerSecret, err = ExtractRulerSecret(&rs, rulerConfig.RemoteWriteSpec.ClientSpec.AuthorizationType)
		if err != nil {
			return nil, nil, ruler, ocpOpts, &status.DegradedError{
				Message: "Invalid ruler remote write authorization secret contents",
				Reason:  lokiv1.ReasonInvalidRulerSecret,
				Requeue: false,
			}
		}
	}

	ocpAmEnabled, err := openshift.AlertManagerSVCExists(ctx, stack.Spec, k)
	if err != nil {
		log.Error(err, "failed to check OCP AlertManager")
		return nil, nil, ruler, ocpOpts, err
	}

	ocpUWAmEnabled, err := openshift.UserWorkloadAlertManagerSVCExists(ctx, stack.Spec, k)
	if err != nil {
		log.Error(err, "failed to check OCP User Workload AlertManager")
		return nil, nil, ruler, ocpOpts, err
	}

	ruler = manifests.Ruler{
		Spec:   rulerConfig,
		Secret: rulerSecret,
	}

	ocpOpts = manifestsocp.Options{
		BuildOpts: manifestsocp.BuildOptions{
			AlertManagerEnabled:             ocpAmEnabled,
			UserWorkloadAlertManagerEnabled: ocpUWAmEnabled,
		},
	}

	return alertingRules, recordingRules, ruler, ocpOpts, nil
}

// listRules returns a slice of AlertingRules and a slice of RecordingRules for the given spec or an error.
// Three cases apply:
//   - Return only matching rules in the stack namespace if no namespace selector is given.
//   - Return only matching rules in the stack namespace and in namespaces matching the namespace selector.
//   - Return no rules if rules selector does not apply at all.
func listRules(ctx context.Context, k k8s.Client, stackNs string, rs *lokiv1.RulesSpec) ([]lokiv1.AlertingRule, []lokiv1.RecordingRule, error) {
	nsl, err := selectRulesNamespaces(ctx, k, stackNs, rs)
	if err != nil {
		return nil, nil, err
	}

	ar, err := selectAlertingRules(ctx, k, rs)
	if err != nil {
		return nil, nil, err
	}

	var alerts []lokiv1.AlertingRule
	for _, rule := range ar.Items {
		for _, ns := range nsl.Items {
			if rule.Namespace == ns.Name {
				alerts = append(alerts, rule)
				break
			}
		}
	}

	rr, err := selectRecordingRules(ctx, k, rs)
	if err != nil {
		return nil, nil, err
	}

	var recs []lokiv1.RecordingRule
	for _, rule := range rr.Items {
		for _, ns := range nsl.Items {
			if rule.Namespace == ns.Name {
				recs = append(recs, rule)
				break
			}
		}
	}

	return alerts, recs, nil
}

func selectRulesNamespaces(ctx context.Context, k k8s.Client, stackNs string, rs *lokiv1.RulesSpec) (corev1.NamespaceList, error) {
	var stackNamespace corev1.Namespace
	key := client.ObjectKey{Name: stackNs}

	err := k.Get(ctx, key, &stackNamespace)
	if err != nil {
		return corev1.NamespaceList{}, kverrors.Wrap(err, "failed to get LokiStack namespace", "namespace", stackNs)
	}

	nsList := corev1.NamespaceList{Items: []corev1.Namespace{stackNamespace}}

	nsSelector, err := metav1.LabelSelectorAsSelector(rs.NamespaceSelector)
	if err != nil {
		return nsList, kverrors.Wrap(err, "failed to create LokiRule namespace selector", "namespaceSelector", rs.NamespaceSelector)
	}

	var nsl v1.NamespaceList
	err = k.List(ctx, &nsl, &client.MatchingLabelsSelector{Selector: nsSelector})
	if err != nil {
		return nsList, kverrors.Wrap(err, "failed to list namespaces for selector", "namespaceSelector", rs.NamespaceSelector)
	}

	for _, ns := range nsl.Items {
		if ns.Name == stackNs {
			continue
		}

		nsList.Items = append(nsList.Items, ns)
	}

	return nsList, nil
}

func selectAlertingRules(ctx context.Context, k k8s.Client, rs *lokiv1.RulesSpec) (lokiv1.AlertingRuleList, error) {
	rulesSelector, err := metav1.LabelSelectorAsSelector(rs.Selector)
	if err != nil {
		return lokiv1.AlertingRuleList{}, kverrors.Wrap(err, "failed to create AlertingRules selector", "selector", rs.Selector)
	}

	var rl lokiv1.AlertingRuleList
	err = k.List(ctx, &rl, &client.MatchingLabelsSelector{Selector: rulesSelector})
	if err != nil {
		return lokiv1.AlertingRuleList{}, kverrors.Wrap(err, "failed to list AlertingRules for selector", "selector", rs.Selector)
	}

	return rl, nil
}

func selectRecordingRules(ctx context.Context, k k8s.Client, rs *lokiv1.RulesSpec) (lokiv1.RecordingRuleList, error) {
	rulesSelector, err := metav1.LabelSelectorAsSelector(rs.Selector)
	if err != nil {
		return lokiv1.RecordingRuleList{}, kverrors.Wrap(err, "failed to create RecordingRules selector", "selector", rs.Selector)
	}

	var rl lokiv1.RecordingRuleList
	err = k.List(ctx, &rl, &client.MatchingLabelsSelector{Selector: rulesSelector})
	if err != nil {
		return lokiv1.RecordingRuleList{}, kverrors.Wrap(err, "failed to list RecordingRules for selector", "selector", rs.Selector)
	}

	return rl, nil
}

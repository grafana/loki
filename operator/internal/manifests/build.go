package manifests

import (
	"github.com/ViaQ/logerr/v2/kverrors"
	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/manifests/internal"

	"github.com/imdario/mergo"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildAll builds all manifests required to run a Loki Stack
func BuildAll(opts Options) ([]client.Object, error) {
	res := make([]client.Object, 0)

	cm, sha1C, mapErr := LokiConfigMap(opts)
	if mapErr != nil {
		return nil, mapErr
	}
	opts.ConfigSHA1 = sha1C

	distributorObjs, err := BuildDistributor(opts)
	if err != nil {
		return nil, err
	}

	ingesterObjs, err := BuildIngester(opts)
	if err != nil {
		return nil, err
	}

	querierObjs, err := BuildQuerier(opts)
	if err != nil {
		return nil, err
	}

	compactorObjs, err := BuildCompactor(opts)
	if err != nil {
		return nil, err
	}

	queryFrontendObjs, err := BuildQueryFrontend(opts)
	if err != nil {
		return nil, err
	}

	indexGatewayObjs, err := BuildIndexGateway(opts)
	if err != nil {
		return nil, err
	}

	res = append(res, cm)
	res = append(res, distributorObjs...)
	res = append(res, ingesterObjs...)
	res = append(res, querierObjs...)
	res = append(res, compactorObjs...)
	res = append(res, queryFrontendObjs...)
	res = append(res, indexGatewayObjs...)
	res = append(res, BuildLokiGossipRingService(opts.Name))

	if opts.Stack.Rules != nil && opts.Stack.Rules.Enabled {
		rulesCm, err := RulesConfigMap(&opts)
		if err != nil {
			return nil, err
		}

		res = append(res, rulesCm)

		rulerObjs, err := BuildRuler(opts)
		if err != nil {
			return nil, err
		}

		res = append(res, rulerObjs...)
	}

	if opts.Flags.EnableGateway {
		gatewayObjects, err := BuildGateway(opts)
		if err != nil {
			return nil, err
		}

		res = append(res, gatewayObjects...)
	}

	if opts.Flags.EnableServiceMonitors {
		res = append(res, BuildServiceMonitors(opts)...)
	}

	if opts.Flags.EnablePrometheusAlerts {
		prometheusRuleObjs, err := BuildPrometheusRule(opts)
		if err != nil {
			return nil, err
		}
		res = append(res, prometheusRuleObjs...)
	}

	return res, nil
}

// DefaultLokiStackSpec returns the default configuration for a LokiStack of
// the specified size
func DefaultLokiStackSpec(size lokiv1beta1.LokiStackSizeType) *lokiv1beta1.LokiStackSpec {
	defaults := internal.StackSizeTable[size]
	return (&defaults).DeepCopy()
}

// ApplyDefaultSettings manipulates the options to conform to
// build specifications
func ApplyDefaultSettings(opts *Options) error {
	spec := DefaultLokiStackSpec(opts.Stack.Size)

	if err := mergo.Merge(spec, opts.Stack, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed merging stack user options", "name", opts.Name)
	}

	strictOverrides := lokiv1beta1.LokiStackSpec{
		Template: &lokiv1beta1.LokiTemplateSpec{
			Compactor: &lokiv1beta1.LokiComponentSpec{
				// Compactor is a singelton application.
				// Only one replica allowed!!!
				Replicas: 1,
			},
		},
	}

	if err := mergo.Merge(spec, strictOverrides, mergo.WithOverride); err != nil {
		return kverrors.Wrap(err, "failed to merge strict defaults")
	}

	opts.ResourceRequirements = internal.ResourceRequirementsTable[opts.Stack.Size]
	opts.Stack = *spec

	return nil
}

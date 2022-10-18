package manifests

import (
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/imdario/mergo"
	openshiftconfigv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/crypto"
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

	if opts.Gates.LokiStackGateway {
		gatewayObjects, err := BuildGateway(opts)
		if err != nil {
			return nil, err
		}

		res = append(res, gatewayObjects...)
	}

	if opts.Stack.Tenants != nil {
		res = configureLokiStackObjsForMode(res, opts)
	}

	if opts.Gates.ServiceMonitors {
		res = append(res, BuildServiceMonitors(opts)...)
	}

	if opts.Gates.LokiStackAlerts {
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
func DefaultLokiStackSpec(size lokiv1.LokiStackSizeType) *lokiv1.LokiStackSpec {
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

	strictOverrides := lokiv1.LokiStackSpec{
		Template: &lokiv1.LokiTemplateSpec{
			Compactor: &lokiv1.LokiComponentSpec{
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

// ApplyTLSSettings manipulates the options to conform to the
// TLS profile specifications
func ApplyTLSSettings(opts *Options, profile *openshiftconfigv1.TLSSecurityProfile) error {
	tlsSecurityProfile := &openshiftconfigv1.TLSSecurityProfile{
		Type: openshiftconfigv1.TLSProfileIntermediateType,
	}

	if profile != nil {
		tlsSecurityProfile = profile
	}

	profileSpec, ok := openshiftconfigv1.TLSProfiles[tlsSecurityProfile.Type]

	if !ok {
		return kverrors.New("unable to determine tls profile settings")
	}

	if tlsSecurityProfile.Type == openshiftconfigv1.TLSProfileCustomType && tlsSecurityProfile.Custom != nil {
		profileSpec = &tlsSecurityProfile.Custom.TLSProfileSpec
	}

	// need to remap all ciphers to their respective IANA names used by Go
	opts.TLSProfile = TLSProfileSpec{
		MinTLSVersion: string(profileSpec.MinTLSVersion),
		Ciphers:       crypto.OpenSSLToIANACipherSuites(profileSpec.Ciphers),
	}

	return nil
}

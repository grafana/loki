package lokistackconfig

import (
	"context"
	"errors"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1beta1 "github.com/grafana/loki/operator/apis/loki/v1beta1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/imdario/mergo"

	"github.com/ViaQ/logerr/v2/kverrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const resourceName = "cluster"

var (
	errUnknownBundleType = errors.New("unknown bundle type")

	communityConfig = lokiv1beta1.LokiStackConfigSpec{
		Gates: lokiv1beta1.FeatureGates{
			LokiStackGateway:              true,
			RestrictedPodSecurityStandard: true,
		},
	}

	communityOpenShiftConfig = lokiv1beta1.LokiStackConfigSpec{
		Gates: lokiv1beta1.FeatureGates{
			ServiceMonitors:            true,
			ServiceMonitorTLSEndpoints: true,
			LokiStackAlerts:            true,
			HTTPEncryption:             true,
			GRPCEncryption:             true,
			BuiltInCertManagement: lokiv1beta1.BuiltInCertManagement{
				Enabled: true,
				// CA certificate validity: 5 years
				CACertValidity: "43830h",
				// CA certificate refresh at 80% of validity
				CACertRefresh: "35064h",
				// Target certificate validity: 90d
				CertValidity: "2160h",
				// Target certificate refresh at 80% of validity
				CertRefresh: "1728h",
			},
			LokiStackGateway:              true,
			RestrictedPodSecurityStandard: true,
			DefaultNodeAffinity:           true,
			OpenShift: lokiv1beta1.OpenShiftFeatureGates{
				Enabled:             true,
				ServingCertsService: true,
				ClusterTLSPolicy:    true,
			},
		},
	}

	openShiftConfig = lokiv1beta1.LokiStackConfigSpec{
		Gates: lokiv1beta1.FeatureGates{
			ServiceMonitors:            true,
			ServiceMonitorTLSEndpoints: true,
			LokiStackAlerts:            true,
			HTTPEncryption:             true,
			GRPCEncryption:             true,
			BuiltInCertManagement: lokiv1beta1.BuiltInCertManagement{
				Enabled: true,
				// CA certificate validity: 5 years
				CACertValidity: "43830h",
				// CA certificate refresh at 80% of validity
				CACertRefresh: "35064h",
				// Target certificate validity: 90d
				CertValidity: "2160h",
				// Target certificate refresh at 80% of validity
				CertRefresh: "1728h",
			},
			LokiStackGateway:              true,
			GrafanaLabsUsageReport:        false,
			RestrictedPodSecurityStandard: true,
			DefaultNodeAffinity:           true,
			OpenShift: lokiv1beta1.OpenShiftFeatureGates{
				Enabled:             true,
				ServingCertsService: true,
				ClusterTLSPolicy:    true,
			},
		},
	}
)

// Get returns a LokiStackConfig object merged with defaults per bundle type when found
// or else the defaults per bundle type.
func Get(ctx context.Context, k k8s.Client, bundleType configv1.BundleType) (*lokiv1beta1.LokiStackConfig, error) {
	var (
		defs  *lokiv1beta1.LokiStackConfig
		stack lokiv1beta1.LokiStackConfig
	)

	defs, err := defaultConfig(bundleType)
	if err != nil {
		return nil, kverrors.Wrap(err, "failed to select default config for bundle type", "bundleType", bundleType)
	}

	if err := k.Get(ctx, client.ObjectKey{Name: resourceName}, &stack); err != nil {
		if apierrors.IsNotFound(err) {
			return defs, nil
		}
		return nil, kverrors.Wrap(err, "failed to lookup lokistackconfig", "name", resourceName)
	}

	if err := mergo.Merge(&defs.Spec, stack.Spec); err != nil {
		return defs, kverrors.Wrap(err, "failed to merge custom lokistackconfig with defaults", "bundleType", bundleType)
	}

	return &stack, nil
}

func defaultConfig(t configv1.BundleType) (*lokiv1beta1.LokiStackConfig, error) {
	lsc := lokiv1beta1.LokiStackConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name: resourceName,
		},
	}

	switch t {
	case configv1.BundleTypeCommunity:
		lsc.Spec = *communityConfig.DeepCopy()
	case configv1.BundleTypeCommunityOpenShift:
		lsc.Spec = *communityOpenShiftConfig.DeepCopy()
	case configv1.BundleTypeOpenShift:
		lsc.Spec = *openShiftConfig.DeepCopy()
	default:
		return nil, errUnknownBundleType
	}

	return lsc.DeepCopy(), nil
}

package lokistackconfig

import (
	"context"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1beta1 "github.com/grafana/loki/operator/apis/loki/v1beta1"
	"github.com/grafana/loki/operator/internal/external/k8s"

	"github.com/ViaQ/logerr/v2/kverrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const resourceName = "cluster"

var (
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

// Get returns the singleton LokiStackConfig custom resource
func Get(ctx context.Context, k k8s.Client, bundleType configv1.BundleType) (*lokiv1beta1.LokiStackConfig, error) {
	var stack lokiv1beta1.LokiStackConfig
	if err := k.Get(ctx, client.ObjectKey{Name: resourceName}, &stack); err != nil {
		if apierrors.IsNotFound(err) {
			return defaultConfig(bundleType), nil
		}
		return nil, kverrors.Wrap(err, "failed to lookup lokistackconfig", "name", resourceName)
	}
	return &stack, nil
}

func defaultConfig(t configv1.BundleType) *lokiv1beta1.LokiStackConfig {
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

	}

	return &lsc
}

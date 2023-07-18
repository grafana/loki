package tlsprofile

import (
	"context"

	lokiv1beta1 "github.com/grafana/loki/operator/apis/loki/v1beta1"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/grafana/loki/operator/internal/external/k8s"
	configv1 "github.com/openshift/api/config/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// APIServerName is the apiserver resource name used to fetch it.
const APIServerName = "cluster"

// GetTLSSecurityProfile gets the tls profile info to apply.
func GetTLSSecurityProfile(ctx context.Context, k k8s.Client, tlsProfileType lokiv1beta1.TLSProfileType) (*configv1.TLSSecurityProfile, error) {
	switch tlsProfileType {
	case lokiv1beta1.TLSProfileOldType:
		return &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileOldType,
		}, nil
	case lokiv1beta1.TLSProfileIntermediateType:
		return &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileIntermediateType,
		}, nil
	case lokiv1beta1.TLSProfileModernType:
		return &configv1.TLSSecurityProfile{
			Type: configv1.TLSProfileModernType,
		}, nil
	default:
		var apiServer configv1.APIServer
		if err := k.Get(ctx, client.ObjectKey{Name: APIServerName}, &apiServer); err != nil {
			return nil, kverrors.Wrap(err, "failed to lookup openshift apiServer")
		}
		return apiServer.Spec.TLSSecurityProfile, nil
	}
}

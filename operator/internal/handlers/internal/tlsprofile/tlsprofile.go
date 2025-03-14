package tlsprofile

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	openshiftconfigv1 "github.com/openshift/api/config/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
)

// APIServerName is the apiserver resource name used to fetch it.
const APIServerName = "cluster"

// GetTLSSecurityProfile gets the tls profile info to apply.
func GetTLSSecurityProfile(ctx context.Context, k k8s.Client, tlsProfileType configv1.TLSProfileType) (*openshiftconfigv1.TLSSecurityProfile, error) {
	switch tlsProfileType {
	case configv1.TLSProfileOldType:
		return &openshiftconfigv1.TLSSecurityProfile{
			Type: openshiftconfigv1.TLSProfileOldType,
		}, nil
	case configv1.TLSProfileIntermediateType:
		return &openshiftconfigv1.TLSSecurityProfile{
			Type: openshiftconfigv1.TLSProfileIntermediateType,
		}, nil
	case configv1.TLSProfileModernType:
		return &openshiftconfigv1.TLSSecurityProfile{
			Type: openshiftconfigv1.TLSProfileModernType,
		}, nil
	default:
		var apiServer openshiftconfigv1.APIServer
		if err := k.Get(ctx, client.ObjectKey{Name: APIServerName}, &apiServer); err != nil {
			return nil, kverrors.Wrap(err, "failed to lookup openshift apiServer")
		}
		return apiServer.Spec.TLSSecurityProfile, nil
	}
}

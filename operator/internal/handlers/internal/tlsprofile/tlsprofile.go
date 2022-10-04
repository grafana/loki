package tlsprofile

import (
	"context"

	projectconfigv1 "github.com/grafana/loki/operator/apis/config/v1"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/grafana/loki/operator/internal/external/k8s"
	openshiftv1 "github.com/openshift/api/config/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// APIServerName is the apiserver resource name used to fetch it.
const APIServerName = "cluster"

// GetSecurityProfileInfo gets the tls profile info to apply.
func GetSecurityProfileInfo(ctx context.Context, k k8s.Client, tlsProfileType projectconfigv1.TLSProfileType) (*openshiftv1.TLSSecurityProfile, error) {
	switch tlsProfileType {
	case projectconfigv1.TLSProfileOldType:
		return &openshiftv1.TLSSecurityProfile{
			Type: openshiftv1.TLSProfileOldType,
		}, nil
	case projectconfigv1.TLSProfileIntermediateType:
		return &openshiftv1.TLSSecurityProfile{
			Type: openshiftv1.TLSProfileIntermediateType,
		}, nil
	case projectconfigv1.TLSProfileModernType:
		return &openshiftv1.TLSSecurityProfile{
			Type: openshiftv1.TLSProfileModernType,
		}, nil
	default:
		var apiServer openshiftv1.APIServer
		if err := k.Get(ctx, client.ObjectKey{Name: APIServerName}, &apiServer); err != nil {
			return nil, kverrors.Wrap(err, "failed to lookup openshift apiServer")
		}
		return apiServer.Spec.TLSSecurityProfile, nil
	}
}

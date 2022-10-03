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
func GetSecurityProfileInfo(ctx context.Context, k k8s.Client, tlsProfileType projectconfigv1.TLSProfileType) (openshiftv1.TLSSecurityProfile, error) {
	var err error

	defaultTLSProfile := openshiftv1.TLSSecurityProfile{
		Type: openshiftv1.TLSProfileIntermediateType,
	}

	if tlsProfileType != "" {
		return openshiftv1.TLSSecurityProfile{
			Type: openshiftv1.TLSProfileType(tlsProfileType),
		}, nil
	}

	var apiServer openshiftv1.APIServer
	err = k.Get(ctx, client.ObjectKey{Name: APIServerName}, &apiServer)

	if err != nil {
		return defaultTLSProfile, kverrors.Wrap(err, "failed to lookup openshift apiServer")
	}

	if apiServer.Spec.TLSSecurityProfile != nil {
		return *apiServer.Spec.TLSSecurityProfile, nil
	}

	return defaultTLSProfile, nil
}

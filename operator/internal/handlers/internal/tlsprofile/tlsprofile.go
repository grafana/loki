package tlsprofile

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	projectconfig_types "github.com/grafana/loki/operator/apis/config/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	openshiftv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/crypto"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetSecurityProfileInfo gets the tls profile info to apply.
func GetSecurityProfileInfo(ctx context.Context, k k8s.Client, tlsProfileType projectconfig_types.TLSProfileType) (projectconfig_types.TLSProfileSpec, error) {
	var tlsProfile openshiftv1.TLSSecurityProfile

	if tlsProfileType != "" {
		tlsProfile = openshiftv1.TLSSecurityProfile{
			Type: openshiftv1.TLSProfileType(tlsProfileType),
		}
	} else {
		tlsProfile = openshiftv1.TLSSecurityProfile{
			Type: openshiftv1.TLSProfileIntermediateType,
		}

		var apiServer openshiftv1.APIServer
		if err := k.Get(ctx, client.ObjectKey{Name: "cluster"}, &apiServer); err != nil {
			if !apierrors.IsNotFound(err) {
				return projectconfig_types.TLSProfileSpec{}, kverrors.Wrap(err, "failed to lookup apiServer")
			}
		}

		if apiServer.Spec.TLSSecurityProfile != nil {
			tlsProfile = *apiServer.Spec.TLSSecurityProfile
		}
	}

	tlsMinVersion, ciphers := extractInfoFromTLSProfile(&tlsProfile)
	return projectconfig_types.TLSProfileSpec{
		MinTLSVersion: tlsMinVersion,
		Ciphers:       ciphers,
	}, nil
}

func extractInfoFromTLSProfile(profile *openshiftv1.TLSSecurityProfile) (string, []string) {
	var profileType openshiftv1.TLSProfileType
	if profile == nil {
		profileType = openshiftv1.TLSProfileIntermediateType
	} else {
		profileType = profile.Type
	}

	var profileSpec *openshiftv1.TLSProfileSpec
	if profileType == openshiftv1.TLSProfileCustomType {
		if profile.Custom != nil {
			profileSpec = &profile.Custom.TLSProfileSpec
		}
	} else {
		profileSpec = openshiftv1.TLSProfiles[profileType]
	}

	// nothing found / custom type set but no actual custom spec
	if profileSpec == nil {
		profileSpec = openshiftv1.TLSProfiles[openshiftv1.TLSProfileIntermediateType]
	}

	// need to remap all Ciphers to their respective IANA names used by Go
	return string(profileSpec.MinTLSVersion), crypto.OpenSSLToIANACipherSuites(profileSpec.Ciphers)
}

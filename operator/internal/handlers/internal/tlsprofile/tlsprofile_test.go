package tlsprofile_test

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	projectconfigv1 "github.com/grafana/loki/operator/apis/config/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/handlers/internal/tlsprofile"
	openshiftv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	apiServer = openshiftv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	}
	ciphersOld = []string{
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
		"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256",
		"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256",
		"TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA",
		"TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA",
		"TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA",
		"TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA",
		"TLS_RSA_WITH_AES_128_GCM_SHA256",
		"TLS_RSA_WITH_AES_256_GCM_SHA384",
		"TLS_RSA_WITH_AES_128_CBC_SHA256",
		"TLS_RSA_WITH_AES_128_CBC_SHA",
		"TLS_RSA_WITH_AES_256_CBC_SHA",
		"TLS_RSA_WITH_3DES_EDE_CBC_SHA",
	}
	ciphersIntermediate = []string{
		"TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256",
		"TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256",
		"TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384",
		"TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384",
		"TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256",
		"TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
	}
)

func TestGetSecurityProfileInfo(t *testing.T) {
	type tt struct {
		desc     string
		profile  projectconfigv1.TLSProfileType
		expected projectconfigv1.TLSProfileSpec
	}

	tc := []tt{
		{
			desc:    "Old profile",
			profile: projectconfigv1.TLSProfileOldType,
			expected: projectconfigv1.TLSProfileSpec{
				MinTLSVersion: "VersionTLS10",
				Ciphers:       ciphersOld,
			},
		},
		{
			desc:    "Intermediate profile",
			profile: projectconfigv1.TLSProfileIntermediateType,
			expected: projectconfigv1.TLSProfileSpec{
				MinTLSVersion: "VersionTLS12",
				Ciphers:       ciphersIntermediate,
			},
		},
		{
			desc:    "Modern profile",
			profile: projectconfigv1.TLSProfileModernType,
			expected: projectconfigv1.TLSProfileSpec{
				MinTLSVersion: "VersionTLS13",
				// Go lib crypto doesn't allow ciphers to be configured for TLS 1.3
				// (Read this and weep: https://github.com/golang/go/issues/29349)
				Ciphers: []string{},
			},
		},
	}

	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if apiServer.Name == name.Name {
			k.SetClientObject(object, &apiServer)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	for _, tc := range tc {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			info, err := tlsprofile.GetSecurityProfileInfo(context.TODO(), k, logr.Logger{}, tc.profile)
			assert.Nil(t, err)
			assert.NotNil(t, info)
			assert.EqualValues(t, tc.expected, info)
		})
	}
}

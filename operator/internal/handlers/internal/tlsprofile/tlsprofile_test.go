package tlsprofile_test

import (
	"context"
	"testing"

	openshiftconfigv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/grafana/loki/operator/api/config/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/handlers/internal/tlsprofile"
)

func TestGetTLSSecurityProfile(t *testing.T) {
	type tt struct {
		desc     string
		profile  configv1.TLSProfileType
		expected openshiftconfigv1.TLSSecurityProfile
	}

	tc := []tt{
		{
			desc:    "Old profile",
			profile: configv1.TLSProfileOldType,
			expected: openshiftconfigv1.TLSSecurityProfile{
				Type: openshiftconfigv1.TLSProfileOldType,
			},
		},
		{
			desc:    "Intermediate profile",
			profile: configv1.TLSProfileIntermediateType,
			expected: openshiftconfigv1.TLSSecurityProfile{
				Type: openshiftconfigv1.TLSProfileIntermediateType,
			},
		},
		{
			desc:    "Modern profile",
			profile: configv1.TLSProfileModernType,
			expected: openshiftconfigv1.TLSSecurityProfile{
				Type: openshiftconfigv1.TLSProfileModernType,
			},
		},
	}

	apiServer := openshiftconfigv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	}

	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if apiServer.Name == name.Name {
			k.SetClientObject(object, &apiServer)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	for _, tc := range tc {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			profile, err := tlsprofile.GetTLSSecurityProfile(context.TODO(), k, tc.profile)

			assert.Nil(t, err)
			assert.NotNil(t, profile)
			assert.EqualValues(t, &tc.expected, profile)
		})
	}
}

func TestGetTLSSecurityProfile_CustomProfile(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	tlsCustomProfile := &openshiftconfigv1.TLSSecurityProfile{
		Type: openshiftconfigv1.TLSProfileCustomType,
		Custom: &openshiftconfigv1.CustomTLSProfile{
			TLSProfileSpec: openshiftconfigv1.TLSProfileSpec{
				Ciphers:       []string{"custom-cipher"},
				MinTLSVersion: "VersionTLS12",
			},
		},
	}

	apiServer := openshiftconfigv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Spec: openshiftconfigv1.APIServerSpec{
			TLSSecurityProfile: tlsCustomProfile,
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if apiServer.Name == name.Name {
			k.SetClientObject(object, &apiServer)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	profile, err := tlsprofile.GetTLSSecurityProfile(context.TODO(), k, configv1.TLSProfileType("custom"))

	assert.Nil(t, err)
	assert.NotNil(t, profile)
	assert.EqualValues(t, tlsCustomProfile, profile)
}

func TestGetTLSSecurityProfile_APIServerNotFound(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	profile, err := tlsprofile.GetTLSSecurityProfile(context.TODO(), k, "")

	assert.NotNil(t, err)
	assert.Nil(t, profile)
}

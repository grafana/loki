package tlsprofile_test

import (
	"context"
	"testing"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/handlers/internal/tlsprofile"

	openshiftconfigv1 "github.com/openshift/api/config/v1"
	"github.com/stretchr/testify/assert"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	apiServer = openshiftconfigv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
	}
)

func TestGetSecurityProfileInfo(t *testing.T) {
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

			info, err := tlsprofile.GetSecurityProfileInfo(context.TODO(), k, tc.profile)

			assert.Nil(t, err)
			assert.NotNil(t, info)
			assert.EqualValues(t, &tc.expected, info)
		})
	}
}

func TestGetSecurityProfileInfo_APIServerNotFound(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	info, err := tlsprofile.GetSecurityProfileInfo(context.TODO(), k, "")

	assert.NotNil(t, err)
	assert.Nil(t, info)
}

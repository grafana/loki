package labelaccess

import (
	"testing"

	"github.com/grafana/dskit/user"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"
)

// TestRequestStreamFilterer_ForRequest tests that ForRequest returns a proper nil interface
// when no LBAC policies are present, and a non-nil filterer when policies exist.
// The nil interface check is critical because a typed nil (*ChunkFilterer)(nil)
// wrapped in an interface would cause `filterer != nil` checks to pass,
// leading to nil pointer dereferences.
func TestRequestStreamFilterer_ForRequest(t *testing.T) {
	for _, tc := range []struct {
		name      string
		policies  LabelPolicySet
		expectNil bool
	}{
		{
			name:      "no LBAC policies returns nil interface",
			policies:  nil,
			expectNil: true,
		},
		{
			name:      "empty LBAC policies returns nil interface",
			policies:  LabelPolicySet{},
			expectNil: true,
		},
		{
			name: "with LBAC policies returns non-nil filterer",
			policies: LabelPolicySet{
				"test-tenant": {
					{Selector: []*types.LabelMatcher{
						{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "prod"},
					}},
				},
			},
			expectNil: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &RequestStreamFilterer{}

			ctx := t.Context()
			if tc.policies != nil {
				ctx = InjectLabelMatchersContext(ctx, tc.policies)
			}
			ctx = user.InjectOrgID(ctx, "test-tenant")

			filterer := r.ForRequest(ctx)

			if tc.expectNil {
				// The interface itself must be nil, not just the underlying value.
				// If we returned a typed nil, `filterer != nil` would be true
				// but filterer.ShouldFilter() would panic.
				require.True(t, filterer == nil, "ForRequest should return nil interface")
			} else {
				require.True(t, filterer != nil, "ForRequest should return non-nil filterer")
			}
		})
	}
}

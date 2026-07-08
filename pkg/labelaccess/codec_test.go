package labelaccess

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/labelaccess/types"
	"github.com/grafana/loki/v3/pkg/querier/queryrange"
	"github.com/grafana/loki/v3/pkg/querier/queryrange/queryrangebase"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
)

// fakeInnerCodecTag is the query-tag enrichment the fake inner codec adds to its
// returned context, used as a canary that the LabelAccess codec must preserve.
const fakeInnerCodecTag = "Source=test"

// fakeInnerCodec embeds queryrange.Codec so it satisfies loki.Codec, but
// overrides QueryRequestUnwrap to return a context that does NOT inherit from
// the input, yet carries an enrichment of its own (a query tag). This isolates
// the contract under test in TestCodec_QueryRequestUnwrap_InjectsLBACIntoReturnedCtx:
// the LabelAccess codec must chain LBAC onto the inner codec's *returned* context
// — preserving its contents — rather than rebuilding from the discarded input ctx.
type fakeInnerCodec struct {
	queryrange.Codec
}

func (f *fakeInnerCodec) QueryRequestUnwrap(ctx context.Context, req *queryrange.QueryRequest) (queryrangebase.Request, context.Context, error) {
	unwrapped, _, err := f.Codec.QueryRequestUnwrap(ctx, req)
	return unwrapped, httpreq.InjectQueryTags(context.Background(), fakeInnerCodecTag), err
}

// TestCodec_QueryRequestUnwrap_InjectsLBACIntoReturnedCtx pins the regression
// fixed in #5180. The buggy code injected LBAC into the input ctx and then
// returned the inner codec's ctx unchanged; downstream handlers received the
// inner ctx without LBAC, and Volume queries silently bypassed label-based
// access control. The fix injects LBAC into the ctx returned by the inner
// codec, which is what callers actually use.
func TestCodec_QueryRequestUnwrap_InjectsLBACIntoReturnedCtx(t *testing.T) {
	policySet := LabelPolicySet{
		"test_tenant": []*types.LabelPolicy{
			{Selector: []*types.LabelMatcher{
				{Type: types.LABEL_MATCHER_TYPE_EQ, Name: "env", Value: "dev"},
			}},
		},
	}

	encoded, err := json.Marshal(policySet)
	require.NoError(t, err)

	req := &queryrange.QueryRequest{
		Metadata: map[string]string{
			HTTPHeaderKey: string(encoded),
		},
		Request: &queryrange.QueryRequest_Series{
			Series: &queryrange.LokiSeriesRequest{},
		},
	}

	codec := NewCodec(&fakeInnerCodec{Codec: queryrange.Codec{}})

	_, returnedCtx, err := codec.QueryRequestUnwrap(context.Background(), req)
	require.NoError(t, err)

	// The fix must inject LBAC into the inner codec's returned ctx; otherwise
	// extraction here fails.
	extracted, err := ExtractLabelMatchersContext(returnedCtx)
	require.NoError(t, err, "LBAC must be reachable from the returned ctx — the codec injected into the wrong ctx")

	require.Contains(t, extracted, "test_tenant")
	require.Len(t, extracted["test_tenant"], 1)
	require.Len(t, extracted["test_tenant"][0].Selector, 1)
	assert.Equal(t, "env", extracted["test_tenant"][0].Selector[0].Name)
	assert.Equal(t, "dev", extracted["test_tenant"][0].Selector[0].Value)

	// The inner codec's own ctx enrichment (here, a query tag) must survive
	// alongside LBAC. Rebuilding from the discarded input ctx drops it.
	assert.Equal(t, fakeInnerCodecTag, httpreq.ExtractQueryTagsFromContext(returnedCtx),
		"inner codec ctx enrichment must be preserved — LBAC was chained off the wrong ctx")
}

// TestCodec_QueryRequestUnwrap_NoMetadataIsPassthrough verifies that when no
// LBAC metadata is present, QueryRequestUnwrap simply returns the inner
// codec's ctx (with no LBAC injection that would mask the absence of policy).
func TestCodec_QueryRequestUnwrap_NoMetadataIsPassthrough(t *testing.T) {
	req := &queryrange.QueryRequest{
		Request: &queryrange.QueryRequest_Series{
			Series: &queryrange.LokiSeriesRequest{},
		},
	}

	codec := NewCodec(&fakeInnerCodec{Codec: queryrange.Codec{}})

	_, returnedCtx, err := codec.QueryRequestUnwrap(context.Background(), req)
	require.NoError(t, err)

	_, err = ExtractLabelMatchersContext(returnedCtx)
	assert.ErrorIs(t, err, errNoMatcherSource, "no LBAC metadata should leave the ctx with no headers")
}

// TestCodec_QueryRequestUnwrap_InvalidMetadataReturnsError verifies that
// malformed LBAC metadata is reported as an error rather than silently
// ignored — a prerequisite for the fix's correctness.
func TestCodec_QueryRequestUnwrap_InvalidMetadataReturnsError(t *testing.T) {
	req := &queryrange.QueryRequest{
		Metadata: map[string]string{
			HTTPHeaderKey: "not-valid-json",
		},
		Request: &queryrange.QueryRequest_Series{
			Series: &queryrange.LokiSeriesRequest{},
		},
	}

	codec := NewCodec(&fakeInnerCodec{Codec: queryrange.Codec{}})

	_, _, err := codec.QueryRequestUnwrap(context.Background(), req)
	require.Error(t, err)
}

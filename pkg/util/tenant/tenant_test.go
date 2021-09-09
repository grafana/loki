package tenant

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/weaveworks/common/user"
)

func strptr(s string) *string {
	return &s
}

type resolverTestCase struct {
	name         string
	headerValue  *string
	errTenantID  error
	errTenantIDs error
	tenantID     string
	tenantIDs    []string
}

func (tc *resolverTestCase) test(r Resolver) func(t *testing.T) {
	return func(t *testing.T) {

		ctx := context.Background()
		if tc.headerValue != nil {
			ctx = user.InjectOrgID(ctx, *tc.headerValue)
		}

		tenantID, err := r.TenantID(ctx)
		if tc.errTenantID != nil {
			assert.Equal(t, tc.errTenantID, err)
		} else {
			assert.NoError(t, err)
			assert.Equal(t, tc.tenantID, tenantID)
		}

		tenantIDs, err := r.TenantIDs(ctx)
		if tc.errTenantIDs != nil {
			assert.Equal(t, tc.errTenantIDs, err)
		} else {
			assert.NoError(t, err)
			assert.Equal(t, tc.tenantIDs, tenantIDs)
		}
	}
}

var commonResolverTestCases = []resolverTestCase{
	{
		name:         "no-header",
		errTenantID:  user.ErrNoOrgID,
		errTenantIDs: user.ErrNoOrgID,
	},
	{
		name:        "empty",
		headerValue: strptr(""),
		tenantIDs:   []string{""},
	},
	{
		name:        "single-tenant",
		headerValue: strptr("tenant-a"),
		tenantID:    "tenant-a",
		tenantIDs:   []string{"tenant-a"},
	},
	{
		name:         "parent-dir",
		headerValue:  strptr(".."),
		errTenantID:  errInvalidTenantID,
		errTenantIDs: errInvalidTenantID,
	},
	{
		name:         "current-dir",
		headerValue:  strptr("."),
		errTenantID:  errInvalidTenantID,
		errTenantIDs: errInvalidTenantID,
	},
}

func TestSingleResolver(t *testing.T) {
	r := &singleResolver{}
	for _, tc := range append(commonResolverTestCases, []resolverTestCase{
		{
			name:        "multi-tenant",
			headerValue: strptr("tenant-a|tenant-b"),
			tenantID:    "tenant-a|tenant-b",
			tenantIDs:   []string{"tenant-a|tenant-b"},
		},
		{
			name:         "containing-forward-slash",
			headerValue:  strptr("forward/slash"),
			errTenantID:  errInvalidTenantID,
			errTenantIDs: errInvalidTenantID,
		},
		{
			name:         "containing-backward-slash",
			headerValue:  strptr(`backward\slash`),
			errTenantID:  errInvalidTenantID,
			errTenantIDs: errInvalidTenantID,
		},
	}...) {
		t.Run(tc.name, tc.test(r))
	}
}

func TestValidTenantID(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  *string
	}{
		{
			name: "tenant-a",
		},
		{
			name: "ABCDEFGHIJKLMNOPQRSTUVWXYZ-abcdefghijklmnopqrstuvwxyz_0987654321!.*'()",
		},
		{
			name: "invalid|",
			err:  strptr("tenant ID 'invalid|' contains unsupported character '|'"),
		},
		{
			name: strings.Repeat("a", 150),
		},
		{
			name: strings.Repeat("a", 151),
			err:  strptr("tenant ID is too long: max 150 characters"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validTenantID(tc.name)
			if tc.err == nil {
				assert.Nil(t, err)
			} else {
				assert.EqualError(t, err, *tc.err)
			}
		})
	}
}

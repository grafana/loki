package goldfish

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestQueryFilter_BuildWhereClause(t *testing.T) {
	tests := []struct {
		name          string
		filter        QueryFilter
		expectedWhere string
		expectedArgs  []any
	}{
		{
			name: "no filters",
			filter: QueryFilter{
				Outcome: OutcomeAll,
			},
			expectedWhere: "",
			expectedArgs:  nil,
		},
		{
			name: "tenant filter only",
			filter: QueryFilter{
				Outcome: OutcomeAll,
				Tenant:  "tenant-b",
			},
			expectedWhere: "WHERE sq.tenant_id = ?",
			expectedArgs:  []any{"tenant-b"},
		},
		{
			name: "user filter only",
			filter: QueryFilter{
				Outcome: OutcomeAll,
				User:    "user123",
			},
			expectedWhere: "WHERE sq.user = ?",
			expectedArgs:  []any{"user123"},
		},
		{
			name: "tenant and user filters",
			filter: QueryFilter{
				Outcome: OutcomeAll,
				Tenant:  "tenant-b",
				User:    "user123",
			},
			expectedWhere: "WHERE sq.tenant_id = ? AND sq.user = ?",
			expectedArgs:  []any{"tenant-b", "user123"},
		},
		{
			name: "new engine filter true",
			filter: QueryFilter{
				Outcome:       OutcomeAll,
				UsedNewEngine: boolPtr(true),
			},
			expectedWhere: "WHERE (sq.cell_a_used_new_engine = 1 OR sq.cell_b_used_new_engine = 1)",
			expectedArgs:  nil,
		},
		{
			name: "new engine filter false",
			filter: QueryFilter{
				Outcome:       OutcomeAll,
				UsedNewEngine: boolPtr(false),
			},
			expectedWhere: "WHERE sq.cell_a_used_new_engine = 0 AND sq.cell_b_used_new_engine = 0",
			expectedArgs:  nil,
		},
		{
			name: "all filters combined",
			filter: QueryFilter{
				Outcome:       OutcomeAll,
				Tenant:        "tenant-b",
				User:          "user123",
				UsedNewEngine: boolPtr(true),
			},
			expectedWhere: "WHERE sq.tenant_id = ? AND sq.user = ? AND (sq.cell_a_used_new_engine = 1 OR sq.cell_b_used_new_engine = 1)",
			expectedArgs:  []any{"tenant-b", "user123"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			whereClause, args := buildWhereClause(tt.filter)
			assert.Equal(t, tt.expectedWhere, whereClause)
			assert.Equal(t, tt.expectedArgs, args)
		})
	}
}

func boolPtr(b bool) *bool {
	return &b
}

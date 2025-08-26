package goldfish

import (
	"testing"
	"time"

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
		{
			name: "with time range specified",
			filter: QueryFilter{
				From: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				To:   time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			},
			expectedWhere: "WHERE sq.sampled_at >= ? AND sq.sampled_at <= ?",
			expectedArgs: []any{
				time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "time range with other filters",
			filter: QueryFilter{
				From:   time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				To:     time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
				Tenant: "tenant-a",
				User:   "user123",
			},
			expectedWhere: "WHERE sq.sampled_at >= ? AND sq.sampled_at <= ? AND sq.tenant_id = ? AND sq.user = ?",
			expectedArgs: []any{
				time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
				"tenant-a",
				"user123",
			},
		},
		{
			name: "only From specified",
			filter: QueryFilter{
				From: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			},
			expectedWhere: "WHERE sq.sampled_at >= ?",
			expectedArgs: []any{
				time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "only To specified",
			filter: QueryFilter{
				To: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			},
			expectedWhere: "WHERE sq.sampled_at <= ?",
			expectedArgs: []any{
				time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			},
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

func TestQueryFilter_BuildWhereClauseWithOutcome(t *testing.T) {
	tests := []struct {
		name          string
		filter        QueryFilter
		expectedWhere string
		expectedArgs  []any
	}{
		{
			name: "outcome match filter",
			filter: QueryFilter{
				Outcome: OutcomeMatch,
			},
			expectedWhere: "WHERE (sq.cell_a_status_code >= 200 AND sq.cell_a_status_code < 300 AND sq.cell_b_status_code >= 200 AND sq.cell_b_status_code < 300 AND sq.cell_a_response_hash = sq.cell_b_response_hash)",
			expectedArgs:  nil,
		},
		{
			name: "outcome mismatch filter",
			filter: QueryFilter{
				Outcome: OutcomeMismatch,
			},
			expectedWhere: "WHERE (sq.cell_a_status_code >= 200 AND sq.cell_a_status_code < 300 AND sq.cell_b_status_code >= 200 AND sq.cell_b_status_code < 300 AND sq.cell_a_response_hash != sq.cell_b_response_hash)",
			expectedArgs:  nil,
		},
		{
			name: "outcome error filter",
			filter: QueryFilter{
				Outcome: OutcomeError,
			},
			expectedWhere: "WHERE (sq.cell_a_status_code < 200 OR sq.cell_a_status_code >= 300 OR sq.cell_b_status_code < 200 OR sq.cell_b_status_code >= 300)",
			expectedArgs:  nil,
		},
		{
			name: "outcome all filter (no filtering)",
			filter: QueryFilter{
				Outcome: OutcomeAll,
			},
			expectedWhere: "",
			expectedArgs:  nil,
		},
		{
			name: "outcome match with tenant filter",
			filter: QueryFilter{
				Outcome: OutcomeMatch,
				Tenant:  "tenant-a",
			},
			expectedWhere: "WHERE (sq.cell_a_status_code >= 200 AND sq.cell_a_status_code < 300 AND sq.cell_b_status_code >= 200 AND sq.cell_b_status_code < 300 AND sq.cell_a_response_hash = sq.cell_b_response_hash) AND sq.tenant_id = ?",
			expectedArgs:  []any{"tenant-a"},
		},
		{
			name: "outcome error with time range",
			filter: QueryFilter{
				Outcome: OutcomeError,
				From:    time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				To:      time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			},
			expectedWhere: "WHERE sq.sampled_at >= ? AND sq.sampled_at <= ? AND (sq.cell_a_status_code < 200 OR sq.cell_a_status_code >= 300 OR sq.cell_b_status_code < 200 OR sq.cell_b_status_code >= 300)",
			expectedArgs: []any{
				time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			},
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

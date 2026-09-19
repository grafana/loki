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
			name:          "no filters",
			filter:        QueryFilter{},
			expectedWhere: "",
			expectedArgs:  nil,
		},
		{
			name: "tenant filter only",
			filter: QueryFilter{
				Tenant: "tenant-b",
			},
			expectedWhere: "WHERE tenant_id = ?",
			expectedArgs:  []any{"tenant-b"},
		},
		{
			name: "user filter only",
			filter: QueryFilter{
				User: "user123",
			},
			expectedWhere: "WHERE user = ?",
			expectedArgs:  []any{"user123"},
		},
		{
			name: "tenant and user filters",
			filter: QueryFilter{
				Tenant: "tenant-b",
				User:   "user123",
			},
			expectedWhere: "WHERE tenant_id = ? AND user = ?",
			expectedArgs:  []any{"tenant-b", "user123"},
		},
		{
			name: "new engine filter true",
			filter: QueryFilter{
				UsedNewEngine: boolPtr(true),
			},
			expectedWhere: "WHERE (cell_a_used_new_engine = 1 OR cell_b_used_new_engine = 1)",
			expectedArgs:  nil,
		},
		{
			name: "new engine filter false",
			filter: QueryFilter{
				UsedNewEngine: boolPtr(false),
			},
			expectedWhere: "WHERE cell_a_used_new_engine = 0 AND cell_b_used_new_engine = 0",
			expectedArgs:  nil,
		},
		{
			name: "logs drilldown filter true",
			filter: QueryFilter{
				IsLogsDrilldown: boolPtr(true),
			},
			expectedWhere: "WHERE is_logs_drilldown = ?",
			expectedArgs:  []any{true},
		},
		{
			name: "logs drilldown filter false",
			filter: QueryFilter{
				IsLogsDrilldown: boolPtr(false),
			},
			expectedWhere: "WHERE is_logs_drilldown = ?",
			expectedArgs:  []any{false},
		},
		{
			name: "all filters combined",
			filter: QueryFilter{
				Tenant:          "tenant-b",
				User:            "user123",
				IsLogsDrilldown: boolPtr(true),
				UsedNewEngine:   boolPtr(true),
			},
			expectedWhere: "WHERE tenant_id = ? AND user = ? AND is_logs_drilldown = ? AND (cell_a_used_new_engine = 1 OR cell_b_used_new_engine = 1)",
			expectedArgs:  []any{"tenant-b", "user123", true},
		},
		{
			name: "with time range specified",
			filter: QueryFilter{
				From: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				To:   time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			},
			expectedWhere: "WHERE sampled_at >= ? AND sampled_at <= ?",
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
			expectedWhere: "WHERE sampled_at >= ? AND sampled_at <= ? AND tenant_id = ? AND user = ?",
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
			expectedWhere: "WHERE sampled_at >= ?",
			expectedArgs: []any{
				time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "only To specified",
			filter: QueryFilter{
				To: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			},
			expectedWhere: "WHERE sampled_at <= ?",
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

func boolPtr(b bool) *bool {
	return &b
}

func TestStatsFilter_BuildStatsWhereClause(t *testing.T) {
	tests := []struct {
		name          string
		filter        StatsFilter
		expectedWhere string
		expectedArgs  []any
	}{
		{
			name:          "no filters - usesRecentData true by default",
			filter:        StatsFilter{UsesRecentData: true},
			expectedWhere: "",
			expectedArgs:  nil,
		},
		{
			name: "time range only",
			filter: StatsFilter{
				From:           time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				To:             time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
				UsesRecentData: true,
			},
			expectedWhere: "WHERE sampled_at >= ? AND sampled_at <= ?",
			expectedArgs: []any{
				time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "usesRecentData false - adds end_time filter",
			filter: StatsFilter{
				UsesRecentData: false,
			},
			expectedWhere: "WHERE end_time <= DATE_SUB(NOW(), INTERVAL 3 HOUR)",
			expectedArgs:  nil,
		},
		{
			name: "time range with usesRecentData false",
			filter: StatsFilter{
				From:           time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				To:             time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
				UsesRecentData: false,
			},
			expectedWhere: "WHERE sampled_at >= ? AND sampled_at <= ? AND end_time <= DATE_SUB(NOW(), INTERVAL 3 HOUR)",
			expectedArgs: []any{
				time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "only From specified",
			filter: StatsFilter{
				From:           time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
				UsesRecentData: true,
			},
			expectedWhere: "WHERE sampled_at >= ?",
			expectedArgs: []any{
				time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
			},
		},
		{
			name: "only To specified",
			filter: StatsFilter{
				To:             time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
				UsesRecentData: true,
			},
			expectedWhere: "WHERE sampled_at <= ?",
			expectedArgs: []any{
				time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			whereClause, args := buildStatsWhereClause(tt.filter)
			assert.Equal(t, tt.expectedWhere, whereClause)
			assert.Equal(t, tt.expectedArgs, args)
		})
	}
}

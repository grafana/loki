package reader

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildLabelSelector(t *testing.T) {
	tests := []struct {
		name     string
		labels   string
		sName    string
		sValue   string
		lName    string
		lVal     string
		expected string
		wantErr  bool
	}{
		{
			name:     "uses legacy params when labels is empty",
			labels:   "",
			sName:    "stream",
			sValue:   "stdout",
			lName:    "pod",
			lVal:     "loki-canary-abc",
			expected: `{stream="stdout",pod="loki-canary-abc"}`,
		},
		{
			name:     "uses labels param when set",
			labels:   "service_name=containerd,namespace=loki,container=loki-canary",
			sName:    "stream",
			sValue:   "stdout",
			lName:    "pod",
			lVal:     "loki-canary-abc",
			expected: `{service_name="containerd",namespace="loki",container="loki-canary"}`,
		},
		{
			name:     "single label",
			labels:   "app=loki",
			sName:    "stream",
			sValue:   "stdout",
			lName:    "pod",
			lVal:     "x",
			expected: `{app="loki"}`,
		},
		{
			name:     "label value containing equals sign",
			labels:   "env=key=value",
			sName:    "stream",
			sValue:   "stdout",
			lName:    "pod",
			lVal:     "x",
			expected: `{env="key=value"}`,
		},
		{
			name:    "invalid label format returns error",
			labels:  "badlabel",
			sName:   "stream",
			sValue:  "stdout",
			lName:   "pod",
			lVal:    "x",
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildLabelSelector(tc.labels, tc.sName, tc.sValue, tc.lName, tc.lVal)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expected, got)
		})
	}
}

func TestBuildMetricQuery(t *testing.T) {
	r := &Reader{
		labelSelector: `{service_name="containerd",namespace="loki"}`,
	}

	tests := []struct {
		name        string
		queryAppend string
		queryRange  string
		expected    string
	}{
		{
			name:        "no query-append",
			queryAppend: "",
			queryRange:  "5m",
			expected:    `count_over_time({service_name="containerd",namespace="loki"}[5m])`,
		},
		{
			name:        "with query-append filter",
			queryAppend: `| pod="loki-canary-abc"`,
			queryRange:  "5m",
			expected:    `count_over_time({service_name="containerd",namespace="loki"} | pod="loki-canary-abc"[5m])`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r.queryAppend = tc.queryAppend
			got := r.buildMetricQuery(tc.queryRange)
			require.Equal(t, tc.expected, got)
		})
	}
}

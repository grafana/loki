package worker

import (
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unsafe"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

// TestMetricInventory pins the exact set of metrics this package owns and the
// label keys each carries. It guards the observability question tree: dropping,
// renaming, or relabeling any of these metrics fails the test so the change is
// deliberate. The actual set is derived by reflecting over the metrics struct's
// collector fields, so a newly added metric that is missing from the expected
// map fails too. Label values are checked separately by the label-bound tests.
func TestMetricInventory(t *testing.T) {
	expected := map[string][]string{
		"loki_engine_worker_bytes_downloaded_total":     nil,
		"loki_engine_worker_comm_site_wait_seconds":     {"message_type", "mode", "outcome", "site", "task_type"},
		"loki_engine_worker_handler_phase_seconds":      {"message_type", "outcome", "phase"},
		"loki_engine_worker_job_handoff_seconds":        {"outcome"},
		"loki_engine_worker_lock_hold_seconds":          {"lock", "mode", "reason"},
		"loki_engine_worker_lock_wait_seconds":          {"lock", "mode", "reason"},
		"loki_engine_worker_operator_rows_in_total":     {"operator_type"},
		"loki_engine_worker_operator_rows_out_total":    {"operator_type"},
		"loki_engine_worker_operator_self_seconds":      {"operator_type"},
		"loki_engine_worker_pages_downloaded_total":     nil,
		"loki_engine_worker_pages_pruned_total":         nil,
		"loki_engine_worker_pass_read_seconds":          nil,
		"loki_engine_worker_pass_send_seconds":          nil,
		"loki_engine_worker_rejected_assignments_total": nil,
		"loki_engine_worker_setup_seconds":              {"task_type"},
		"loki_engine_worker_slot_phase_seconds_total":   {"outcome", "phase", "task_type"},
		"loki_engine_worker_slot_ready_wait_seconds":    {"outcome"},
		"loki_engine_worker_status_update_errors_total": {"error_class"},
		"loki_engine_worker_status_update_seconds":      nil,
		"loki_engine_worker_task_exec_seconds":          {"task_type"},
		"loki_engine_worker_task_open_seconds":          {"task_type"},
		"loki_engine_worker_task_read_seconds":          {"task_type"},
		"loki_engine_worker_task_send_seconds":          {"task_type"},
		"loki_engine_worker_tasks_assigned_total":       nil,
	}

	require.Equal(t, expected, metricInventory(t, newMetrics()))
}

// describer is satisfied by prometheus collectors and by *obslock.Metrics.
type describer interface {
	Describe(chan<- *prometheus.Desc)
}

var descLabelsRe = regexp.MustCompile(`fqName: "([^"]+)".*variableLabels: \{([^}]*)\}`)

// metricInventory reflects over every collector field of a metrics value
// (including the shared obslock lock metrics) and returns each registered
// metric's fully-qualified name mapped to its sorted label keys.
func metricInventory(t *testing.T, m any) map[string][]string {
	t.Helper()

	describers := collectDescribers(reflect.ValueOf(m).Elem())

	ch := make(chan *prometheus.Desc)
	go func() {
		defer close(ch)
		for _, d := range describers {
			d.Describe(ch)
		}
	}()

	inventory := make(map[string][]string)
	for desc := range ch {
		match := descLabelsRe.FindStringSubmatch(desc.String())
		require.NotNilf(t, match, "could not parse descriptor %q", desc.String())

		var keys []string
		for _, key := range strings.Split(match[2], ",") {
			if key != "" {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		inventory[match[1]] = keys
	}
	return inventory
}

// collectDescribers returns every struct field of v that is a metric collector,
// reading unexported fields via reflection so the inventory stays authoritative
// without a hand-maintained accessor.
func collectDescribers(v reflect.Value) []describer {
	var out []describer
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if !f.CanAddr() {
			continue
		}
		fv := reflect.NewAt(f.Type(), unsafe.Pointer(f.UnsafeAddr())).Elem().Interface()
		if d, ok := fv.(describer); ok && d != nil {
			out = append(out, d)
		}
	}
	return out
}

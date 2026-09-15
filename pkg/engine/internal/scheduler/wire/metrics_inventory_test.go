package wire

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
// deliberate. The actual set is derived by reflecting over the Metrics struct's
// collector fields, so a newly added metric that is missing from the expected
// map fails too. Label values are checked separately by the label-bound tests.
func TestMetricInventory(t *testing.T) {
	expected := map[string][]string{
		"loki_engine_scheduler_wire_connections_active":        {"transport"},
		"loki_engine_scheduler_wire_frame_bytes_total":         {"direction", "frame_type", "message_type", "mode"},
		"loki_engine_scheduler_wire_frame_codec_stage_seconds": {"frame_type", "message_type", "operation", "stage"},
		"loki_engine_scheduler_wire_frame_queue_length":        {"frame_type", "message_type", "mode", "queue"},
		"loki_engine_scheduler_wire_frame_queue_wait_seconds":  {"frame_type", "message_type", "mode", "queue"},
		"loki_engine_scheduler_wire_frame_receive_seconds":     {"frame_type", "message_type", "mode", "phase", "transport"},
		"loki_engine_scheduler_wire_frame_send_seconds":        {"frame_type", "message_type", "mode", "phase", "transport"},
		"loki_engine_scheduler_wire_frame_size_bytes":          {"direction", "frame_type", "message_type", "mode"},
		"loki_engine_scheduler_wire_frames_received_total":     {"message_type", "type"},
		"loki_engine_scheduler_wire_handler_seconds":           {"message_type", "outcome"},
		"loki_engine_scheduler_wire_lock_hold_seconds":         {"lock", "mode", "reason"},
		"loki_engine_scheduler_wire_lock_wait_seconds":         {"lock", "mode", "reason"},
		"loki_engine_scheduler_wire_message_roundtrip_seconds": {"message_type", "mode", "outcome"},
		"loki_engine_scheduler_wire_messages_sent_total":       {"message_type", "mode"},
		"loki_engine_scheduler_wire_queue_blocked_senders":     {"frame_type", "message_type", "mode", "queue"},
		"loki_engine_scheduler_wire_write_busy_seconds_total":  {"transport"},
	}

	require.Equal(t, expected, metricInventory(t, NewMetrics()))
}

// describer is satisfied by prometheus collectors and by *obslock.Metrics.
type describer interface {
	Describe(chan<- *prometheus.Desc)
}

var descLabelsRe = regexp.MustCompile(`fqName: "([^"]+)".*variableLabels: \{([^}]*)\}`)

// metricInventory reflects over every collector field of a *Metrics value
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

package push

import (
	"context"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"

	"github.com/grafana/loki/pkg/push"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// These tests are about the shape the OTLP parser now builds: resource and scope attributes
// stored once on the group that owns them rather than copied onto every entry. The rest of
// the OTLP tests go through flattenRequest and so deliberately cannot see this.

func parseNested(t *testing.T, ld plog.Logs, cfg OTLPConfig) *logproto.InternalPushRequest {
	t.Helper()

	streamResolver := newMockStreamResolver("fake", &fakeLimits{})
	req, err := otlpToLokiPushRequest(
		context.Background(), ld, "fake", cfg, nil, []string{}, NewMockTracker(),
		NewPushStats(), log.NewNopLogger(), streamResolver, constants.OTLP,
	)
	require.NoError(t, err)
	return req
}

// streamByLabels returns the one stream carrying the given labels.
func streamByLabels(t *testing.T, req *logproto.InternalPushRequest, want string) *logproto.InternalStreamAdapter {
	t.Helper()

	for i := range req.Streams {
		if req.Streams[i].Labels == want {
			return &req.Streams[i]
		}
	}
	labels := make([]string, 0, len(req.Streams))
	for i := range req.Streams {
		labels = append(labels, req.Streams[i].Labels)
	}
	t.Fatalf("no stream with labels %s; got %v", want, labels)
	return nil
}

func attrNames(attrs []push.LabelAdapter) []string {
	out := make([]string, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, a.Name)
	}
	return out
}

func TestOTLPStoresSharedAttributesOnceNotPerEntry(t *testing.T) {
	now := time.Unix(0, time.Now().UnixNano())

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "service-1")
	rl.Resource().Attributes().PutStr("host.name", "host-1")
	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().Attributes().PutStr("scope.attr", "value")
	sl.Scope().SetName("scope-1")
	for _, body := range []string{"one", "two", "three"} {
		lr := sl.LogRecords().AppendEmpty()
		lr.Body().SetStr(body)
		lr.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
	}

	req := parseNested(t, ld, DefaultOTLPConfig(defaultGlobalOTLPConfig))
	stream := streamByLabels(t, req, `{service_name="service-1"}`)

	require.Len(t, stream.ResourceLogs, 1, "one resource means one group")
	group := &stream.ResourceLogs[0]
	require.Len(t, group.ScopeLogs, 1, "one scope means one scope group")
	scope := &group.ScopeLogs[0]

	// host.name is not a stream label, so it is structured metadata on the resource.
	require.Equal(t, []string{"host_name"}, attrNames(group.Attrs))
	require.Contains(t, attrNames(scope.Attrs), "scope_attr")

	require.Len(t, scope.Entries, 3)
	for _, e := range scope.Entries {
		require.Empty(t, e.StructuredMetadata,
			"an entry with no attributes of its own must carry nothing: the resource's and scope's are on the group")
	}

	// This is the point of the change: the shared sets are counted once, not three times.
	require.Less(t, stream.UnexpandedSize(), stream.ExpandedSize())
	require.Equal(t, len("one")+len("two")+len("three")+
		logproto.EffectiveMetadataSize(group.Attrs, scope.Attrs, &push.Entry{}),
		stream.UnexpandedSize())
}

func TestOTLPKeepsEachScopeSeparateUnderOneResource(t *testing.T) {
	now := time.Unix(0, time.Now().UnixNano())

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "service-1")
	for _, name := range []string{"scope-a", "scope-b"} {
		sl := rl.ScopeLogs().AppendEmpty()
		sl.Scope().SetName(name)
		sl.Scope().Attributes().PutStr("which", name)
		lr := sl.LogRecords().AppendEmpty()
		lr.Body().SetStr(name + " line")
		lr.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
	}

	req := parseNested(t, ld, DefaultOTLPConfig(defaultGlobalOTLPConfig))
	stream := streamByLabels(t, req, `{service_name="service-1"}`)

	require.Len(t, stream.ResourceLogs, 1, "both scopes share one resource")
	group := &stream.ResourceLogs[0]
	require.Len(t, group.ScopeLogs, 2, "each scope keeps its own attributes")

	for i, want := range []string{"scope-a", "scope-b"} {
		scope := &group.ScopeLogs[i]
		require.Len(t, scope.Entries, 1)
		require.Equal(t, want+" line", scope.Entries[0].Line)

		md := logproto.AppendEffectiveMetadata(nil, group.Attrs, scope.Attrs, &scope.Entries[0])
		require.Contains(t, md, push.LabelAdapter{Name: "which", Value: want},
			"the entry's effective metadata must include its own scope's attributes")
	}
}

func TestOTLPKeepsEachResourceSeparate(t *testing.T) {
	now := time.Unix(0, time.Now().UnixNano())

	// Two resources that produce the same stream labels, so both land in one stream and
	// each must keep its own attributes.
	ld := plog.NewLogs()
	for _, host := range []string{"host-1", "host-2"} {
		rl := ld.ResourceLogs().AppendEmpty()
		rl.Resource().Attributes().PutStr("service.name", "service-1")
		rl.Resource().Attributes().PutStr("host.name", host)
		lr := rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
		lr.Body().SetStr(host + " line")
		lr.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
	}

	req := parseNested(t, ld, DefaultOTLPConfig(defaultGlobalOTLPConfig))
	stream := streamByLabels(t, req, `{service_name="service-1"}`)

	require.Len(t, stream.ResourceLogs, 2,
		"two resources with different attributes cannot share a group, or one host's attributes would be attributed to the other's lines")

	for i, host := range []string{"host-1", "host-2"} {
		group := &stream.ResourceLogs[i]
		scope := &group.ScopeLogs[0]
		require.Equal(t, host+" line", scope.Entries[0].Line)
		require.Contains(t, group.Attrs, push.LabelAdapter{Name: "host_name", Value: host})
	}
}

func TestOTLPEntryPromotedToItsOwnStreamKeepsItsGroupAttributes(t *testing.T) {
	now := time.Unix(0, time.Now().UnixNano())

	// Promoting a log attribute to an index label moves that single entry to a different
	// stream. Its resource and scope attributes have to follow it there.
	cfg := DefaultOTLPConfig(defaultGlobalOTLPConfig)
	cfg.LogAttributes = []AttributesConfig{{Action: IndexLabel, Attributes: []string{"promoted"}}}

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "service-1")
	rl.Resource().Attributes().PutStr("host.name", "host-1")
	sl := rl.ScopeLogs().AppendEmpty()
	sl.Scope().Attributes().PutStr("scope.attr", "value")

	plain := sl.LogRecords().AppendEmpty()
	plain.Body().SetStr("plain line")
	plain.SetTimestamp(pcommon.Timestamp(now.UnixNano()))

	moved := sl.LogRecords().AppendEmpty()
	moved.Body().SetStr("moved line")
	moved.SetTimestamp(pcommon.Timestamp(now.UnixNano()))
	moved.Attributes().PutStr("promoted", "yes")

	req := parseNested(t, ld, cfg)

	base := streamByLabels(t, req, `{service_name="service-1"}`)
	require.Equal(t, 1, base.EntryCount())
	require.Equal(t, "plain line", base.ResourceLogs[0].ScopeLogs[0].Entries[0].Line)

	promoted := streamByLabels(t, req, `{promoted="yes", service_name="service-1"}`)
	require.Equal(t, 1, promoted.EntryCount())

	group := &promoted.ResourceLogs[0]
	scope := &group.ScopeLogs[0]
	require.Equal(t, "moved line", scope.Entries[0].Line)
	require.Contains(t, attrNames(group.Attrs), "host_name",
		"the resource's attributes must follow the entry into its new stream")
	require.Contains(t, attrNames(scope.Attrs), "scope_attr",
		"and so must the scope's")
}

func TestOTLPNativeShapeHasNothingShared(t *testing.T) {
	// A resource whose every attribute becomes a stream label leaves nothing to lift out,
	// so the two size measures agree and flattening is the fast path.
	now := time.Unix(0, time.Now().UnixNano())

	ld := plog.NewLogs()
	rl := ld.ResourceLogs().AppendEmpty()
	rl.Resource().Attributes().PutStr("service.name", "service-1")
	lr := rl.ScopeLogs().AppendEmpty().LogRecords().AppendEmpty()
	lr.Body().SetStr("a line")
	lr.SetTimestamp(pcommon.Timestamp(now.UnixNano()))

	req := parseNested(t, ld, DefaultOTLPConfig(defaultGlobalOTLPConfig))
	stream := streamByLabels(t, req, `{service_name="service-1"}`)

	require.Empty(t, stream.ResourceLogs[0].Attrs)
	require.Empty(t, stream.ResourceLogs[0].ScopeLogs[0].Attrs)
	require.Equal(t, stream.UnexpandedSize(), stream.ExpandedSize())
}

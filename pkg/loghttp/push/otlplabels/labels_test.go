package otlplabels

import (
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/plog"

	"github.com/grafana/loki/pkg/push"
)

func labelAdapterGet(ls push.LabelsAdapter, name string) string {
	for _, l := range ls {
		if l.Name == name {
			return l.Value
		}
	}
	return ""
}

func labelSetGet(ls model.LabelSet, name string) string {
	return string(ls[model.LabelName(name)])
}

// ---------- AttributeToLabels tests ----------

func TestAttributeToLabels_Simple(t *testing.T) {
	v := pcommon.NewValueStr("hello")
	got, err := AttributeToLabels("my.key", v, "")
	require.NoError(t, err)
	require.Equal(t, 1, len(got))
	assert.Equal(t, "hello", labelAdapterGet(got, "my_key"))
}

func TestAttributeToLabels_WithPrefix(t *testing.T) {
	v := pcommon.NewValueStr("val")
	got, err := AttributeToLabels("child", v, "parent")
	require.NoError(t, err)
	require.Equal(t, 1, len(got))
	assert.Equal(t, "val", labelAdapterGet(got, "parent_child"))
}

func TestAttributeToLabels_MapValue(t *testing.T) {
	v := pcommon.NewValueMap()
	m := v.Map()
	m.PutStr("inner.a", "v1")
	m.PutStr("inner.b", "v2")

	got, err := AttributeToLabels("outer", v, "")
	require.NoError(t, err)
	assert.Equal(t, 2, len(got))
	assert.Equal(t, "v1", labelAdapterGet(got, "outer_inner_a"))
	assert.Equal(t, "v2", labelAdapterGet(got, "outer_inner_b"))
}

func TestAttributeToLabels_NestedMap(t *testing.T) {
	v := pcommon.NewValueMap()
	inner := v.Map().PutEmptyMap("level1")
	inner.PutStr("level2", "deep")

	got, err := AttributeToLabels("root", v, "")
	require.NoError(t, err)
	assert.Equal(t, "deep", labelAdapterGet(got, "root_level1_level2"))
}

func TestAttributeToLabels_LeadingDigit(t *testing.T) {
	v := pcommon.NewValueStr("val")
	got, err := AttributeToLabels("123abc", v, "")
	require.NoError(t, err)
	assert.Equal(t, "val", labelAdapterGet(got, "key_123abc"))
}

func TestAttributeToLabels_DotsToUnderscores(t *testing.T) {
	v := pcommon.NewValueStr("val")
	got, err := AttributeToLabels("a.b.c", v, "")
	require.NoError(t, err)
	assert.Equal(t, "val", labelAdapterGet(got, "a_b_c"))
}

func TestAttributeToLabels_ReservedLabel(t *testing.T) {
	v := pcommon.NewValueStr("val")
	got, err := AttributeToLabels("__reserved__", v, "")
	require.NoError(t, err)
	assert.Equal(t, "val", labelAdapterGet(got, "__reserved__"))
}

func TestAttributeToLabels_IntValue(t *testing.T) {
	v := pcommon.NewValueInt(42)
	got, err := AttributeToLabels("count", v, "")
	require.NoError(t, err)
	assert.Equal(t, "42", labelAdapterGet(got, "count"))
}

func TestAttributeToLabels_BoolValue(t *testing.T) {
	v := pcommon.NewValueBool(true)
	got, err := AttributeToLabels("flag", v, "")
	require.NoError(t, err)
	assert.Equal(t, "true", labelAdapterGet(got, "flag"))
}

func TestAttributesToLabels(t *testing.T) {
	for _, tc := range []struct {
		name         string
		buildAttrs   func() pcommon.Map
		expectedResp push.LabelsAdapter
	}{
		{
			name: "no attributes",
			buildAttrs: func() pcommon.Map {
				return pcommon.NewMap()
			},
			expectedResp: push.LabelsAdapter{},
		},
		{
			name: "with attributes",
			buildAttrs: func() pcommon.Map {
				attrs := pcommon.NewMap()
				attrs.PutEmpty("empty")
				attrs.PutStr("str", "val")
				attrs.PutInt("int", 1)
				attrs.PutDouble("double", 3.14)
				attrs.PutBool("bool", true)
				attrs.PutEmptyBytes("bytes").Append(1, 2, 3)

				slice := attrs.PutEmptySlice("slice")
				slice.AppendEmpty().SetInt(1)
				slice.AppendEmpty().SetEmptySlice().AppendEmpty().SetStr("foo")
				slice.AppendEmpty().SetEmptyMap().PutStr("fizz", "buzz")

				m := attrs.PutEmptyMap("nested")
				m.PutStr("foo", "bar")
				m.PutEmptyMap("more").PutStr("key", "val")

				return attrs
			},
			expectedResp: push.LabelsAdapter{
				{Name: "empty"},
				{Name: "str", Value: "val"},
				{Name: "int", Value: "1"},
				{Name: "double", Value: "3.14"},
				{Name: "bool", Value: "true"},
				{Name: "bytes", Value: base64.StdEncoding.EncodeToString([]byte{1, 2, 3})},
				{Name: "slice", Value: `[1,["foo"],{"fizz":"buzz"}]`},
				{Name: "nested_foo", Value: "bar"},
				{Name: "nested_more_key", Value: "val"},
			},
		},
		{
			name: "attributes with special chars",
			buildAttrs: func() pcommon.Map {
				attrs := pcommon.NewMap()
				attrs.PutStr("st.r", "val")

				m := attrs.PutEmptyMap("nest*ed")
				m.PutStr("fo@o", "bar")
				m.PutEmptyMap("m$ore").PutStr("k_ey", "val")

				return attrs
			},
			expectedResp: push.LabelsAdapter{
				{Name: "st_r", Value: "val"},
				{Name: "nest_ed_fo_o", Value: "bar"},
				{Name: "nest_ed_m_ore_k_ey", Value: "val"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lbls, err := attributesToLabels(tc.buildAttrs(), "")
			require.NoError(t, err)
			require.Equal(t, tc.expectedResp, lbls)
		})
	}
}

// ---------- ResourceAttrsToStreamLabels tests ----------

func makeOTLPConfig(indexed []string) OTLPConfig {
	return OTLPConfig{
		ResourceAttributes: ResourceAttributesConfig{
			AttributesConfig: []AttributesConfig{
				{
					Action:     IndexLabel,
					Attributes: indexed,
				},
			},
		},
	}
}

func makeAttrs(kvs map[string]string) pcommon.Map {
	m := pcommon.NewMap()
	for k, v := range kvs {
		m.PutStr(k, v)
	}
	return m
}

func TestResourceAttrsToStreamLabels_DefaultIndexed(t *testing.T) {
	cfg := makeOTLPConfig([]string{"service.name", "deployment.environment"})
	attrs := pcommon.NewMap()
	attrs.PutStr("service.name", "my-service")
	attrs.PutStr("deployment.environment", "production")
	attrs.PutStr("host.name", "node-1")

	got, err := ResourceAttrsToStreamLabels(attrs, cfg, nil)
	require.NoError(t, err)

	assert.Equal(t, "production", labelSetGet(got.StreamLabels, "deployment_environment"))
	assert.Equal(t, "my-service", labelSetGet(got.StreamLabels, "service_name"))
	assert.Equal(t, "", labelSetGet(got.StreamLabels, "host_name"), "non-indexed attribute should be excluded from stream labels")
	assert.Equal(t, "node-1", labelAdapterGet(got.StructuredMetadata, "host_name"), "non-indexed attribute should be structured metadata")
}

func TestResourceAttrsToStreamLabels_NonIndexedAsStructuredMetadata(t *testing.T) {
	cfg := makeOTLPConfig([]string{"service.name"})
	attrs := makeAttrs(map[string]string{
		"service.name": "svc",
		"host.name":    "node-1",
		"host.arch":    "amd64",
	})

	got, err := ResourceAttrsToStreamLabels(attrs, cfg, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, len(got.StreamLabels))
	assert.Equal(t, "svc", labelSetGet(got.StreamLabels, "service_name"))
	assert.Equal(t, 2, len(got.StructuredMetadata))
}

func TestResourceAttrsToStreamLabels_DroppedExcluded(t *testing.T) {
	cfg := OTLPConfig{
		ResourceAttributes: ResourceAttributesConfig{
			AttributesConfig: []AttributesConfig{
				{Action: IndexLabel, Attributes: []string{"service.name"}},
				{Action: Drop, Attributes: []string{"telemetry.sdk.name"}},
			},
		},
	}
	attrs := makeAttrs(map[string]string{
		"service.name":       "svc",
		"telemetry.sdk.name": "opentelemetry",
	})

	got, err := ResourceAttrsToStreamLabels(attrs, cfg, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, len(got.StreamLabels))
	assert.Equal(t, "svc", labelSetGet(got.StreamLabels, "service_name"))
	assert.Equal(t, 0, len(got.StructuredMetadata), "dropped attrs should not appear in structured metadata")
}

func TestResourceAttrsToStreamLabels_ServiceNameDiscovery(t *testing.T) {
	cfg := makeOTLPConfig([]string{"k8s.namespace.name"})
	attrs := makeAttrs(map[string]string{
		"k8s.namespace.name": "my-namespace",
	})

	got, err := ResourceAttrsToStreamLabels(attrs, cfg, []string{"k8s_namespace_name"})
	require.NoError(t, err)
	assert.Equal(t, "my-namespace", labelSetGet(got.StreamLabels, LabelServiceName))
	assert.Equal(t, "my-namespace", labelSetGet(got.StreamLabels, "k8s_namespace_name"))
}

func TestResourceAttrsToStreamLabels_ServiceNameDiscoverySkippedWhenPresent(t *testing.T) {
	cfg := makeOTLPConfig([]string{"service.name", "k8s.namespace.name"})
	attrs := pcommon.NewMap()
	attrs.PutStr("service.name", "explicit-svc")
	attrs.PutStr("k8s.namespace.name", "my-ns")

	got, err := ResourceAttrsToStreamLabels(attrs, cfg, []string{"k8s_namespace_name"})
	require.NoError(t, err)
	assert.Equal(t, "explicit-svc", labelSetGet(got.StreamLabels, "service_name"),
		"service_name should come from the explicit service.name attribute, not discovery")
	assert.Equal(t, "my-ns", labelSetGet(got.StreamLabels, "k8s_namespace_name"))
}

func TestResourceAttrsToStreamLabels_ServiceNameDefaultsToUnknown(t *testing.T) {
	cfg := makeOTLPConfig([]string{"host.name"})
	attrs := makeAttrs(map[string]string{
		"host.name": "node-1",
	})

	got, err := ResourceAttrsToStreamLabels(attrs, cfg, []string{"k8s_namespace_name"})
	require.NoError(t, err)
	assert.Equal(t, ServiceUnknown, labelSetGet(got.StreamLabels, LabelServiceName))
}

func TestResourceAttrsToStreamLabels_NoDiscoveryWhenListEmpty(t *testing.T) {
	cfg := makeOTLPConfig([]string{"host.name"})
	attrs := makeAttrs(map[string]string{
		"host.name": "node-1",
	})

	got, err := ResourceAttrsToStreamLabels(attrs, cfg, nil)
	require.NoError(t, err)
	assert.Equal(t, "", labelSetGet(got.StreamLabels, LabelServiceName), "no discovery when discoverServiceName is nil")
}

func TestResourceAttrsToStreamLabels_EmptyAttrs(t *testing.T) {
	cfg := makeOTLPConfig([]string{"service.name"})
	attrs := pcommon.NewMap()

	got, err := ResourceAttrsToStreamLabels(attrs, cfg, nil)
	require.NoError(t, err)
	assert.Equal(t, 0, len(got.StreamLabels))
}

// ---------- ScopeAttrsToStructuredMetadata tests ----------

func TestScopeAttrsToStructuredMetadata(t *testing.T) {
	t.Run("attributes and scope fields", func(t *testing.T) {
		cfg := DefaultOTLPConfig(defaultGlobalOTLPConfig)
		ld := plog.NewLogs()
		rl := ld.ResourceLogs().AppendEmpty()
		sl := rl.ScopeLogs().AppendEmpty()
		scope := sl.Scope()
		scope.SetName("my-scope")
		scope.SetVersion("1.2.3")
		scope.Attributes().PutStr("op", "buzz")
		scope.SetDroppedAttributesCount(5)

		got, err := ScopeAttrsToStructuredMetadata(rl.ScopeLogs(), 0, cfg)
		require.NoError(t, err)

		assert.Equal(t, "buzz", labelAdapterGet(got.StructuredMetadata, "op"))
		assert.Equal(t, "my-scope", labelAdapterGet(got.StructuredMetadata, "scope_name"))
		assert.Equal(t, "1.2.3", labelAdapterGet(got.StructuredMetadata, "scope_version"))
		assert.Equal(t, "5", labelAdapterGet(got.StructuredMetadata, "scope_dropped_attributes_count"))
	})

	t.Run("dropped scope attributes excluded", func(t *testing.T) {
		cfg := OTLPConfig{
			ScopeAttributes: []AttributesConfig{
				{Action: Drop, Attributes: []string{"drop.me"}},
			},
		}
		ld := plog.NewLogs()
		rl := ld.ResourceLogs().AppendEmpty()
		sl := rl.ScopeLogs().AppendEmpty()
		scope := sl.Scope()
		scope.Attributes().PutStr("drop.me", "gone")
		scope.Attributes().PutStr("keep.me", "here")

		got, err := ScopeAttrsToStructuredMetadata(rl.ScopeLogs(), 0, cfg)
		require.NoError(t, err)

		assert.Equal(t, "", labelAdapterGet(got.StructuredMetadata, "drop_me"))
		assert.Equal(t, "here", labelAdapterGet(got.StructuredMetadata, "keep_me"))
	})

	t.Run("empty scope fields omitted", func(t *testing.T) {
		cfg := DefaultOTLPConfig(defaultGlobalOTLPConfig)
		ld := plog.NewLogs()
		rl := ld.ResourceLogs().AppendEmpty()
		rl.ScopeLogs().AppendEmpty()

		got, err := ScopeAttrsToStructuredMetadata(rl.ScopeLogs(), 0, cfg)
		require.NoError(t, err)
		assert.Equal(t, 0, len(got.StructuredMetadata))
	})
}

// ---------- LogAttrsToLabels tests ----------

func TestLogAttrsToLabels_OnlyBody(t *testing.T) {
	log := plog.NewLogRecord()
	log.Body().SetStr("log body")
	log.SetTimestamp(pcommon.Timestamp(1000000000))

	got, err := LogAttrsToLabels(log, DefaultOTLPConfig(defaultGlobalOTLPConfig))
	require.NoError(t, err)
	assert.Equal(t, 0, len(got.IndexLabels))
	assert.Equal(t, 0, len(got.StructuredMetadata))
}

func TestLogAttrsToLabels_AllFields(t *testing.T) {
	log := plog.NewLogRecord()
	log.Body().SetStr("log body")
	log.SetTimestamp(pcommon.Timestamp(1000000000))
	log.SetObservedTimestamp(pcommon.Timestamp(1000000001))
	log.SetSeverityNumber(plog.SeverityNumberDebug)
	log.SetSeverityText("debug")
	log.SetDroppedAttributesCount(1)
	log.SetFlags(plog.DefaultLogRecordFlags.WithIsSampled(true))
	log.SetTraceID([16]byte{0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78, 0x12, 0x34, 0x56, 0x78})
	log.SetSpanID([8]byte{0x12, 0x23, 0xAD, 0x12, 0x23, 0xAD, 0x12, 0x23})
	log.SetEventName("my.event")
	log.Attributes().PutStr("foo", "bar")

	got, err := LogAttrsToLabels(log, DefaultOTLPConfig(defaultGlobalOTLPConfig))
	require.NoError(t, err)

	assert.Equal(t, "bar", labelAdapterGet(got.StructuredMetadata, "foo"))
	assert.NotEmpty(t, labelAdapterGet(got.StructuredMetadata, "observed_timestamp"))
	assert.Equal(t, "5", labelAdapterGet(got.StructuredMetadata, OTLPSeverityNumber))
	assert.Equal(t, "debug", labelAdapterGet(got.StructuredMetadata, OTLPSeverityText))
	assert.Equal(t, "1", labelAdapterGet(got.StructuredMetadata, "dropped_attributes_count"))
	assert.Equal(t, fmt.Sprintf("%d", plog.DefaultLogRecordFlags.WithIsSampled(true)), labelAdapterGet(got.StructuredMetadata, "flags"))
	assert.Equal(t, "12345678123456781234567812345678", labelAdapterGet(got.StructuredMetadata, "trace_id"))
	assert.Equal(t, "1223ad1223ad1223", labelAdapterGet(got.StructuredMetadata, "span_id"))
	assert.Equal(t, "my.event", labelAdapterGet(got.StructuredMetadata, OTLPEventName))
}

func TestLogAttrsToLabels_EventNameFieldWins(t *testing.T) {
	log := plog.NewLogRecord()
	log.Body().SetStr("log body")
	log.SetTimestamp(pcommon.Timestamp(1000000000))
	log.SetEventName("otlp.field")
	log.Attributes().PutStr(OTLPEventName, "attribute.value")

	got, err := LogAttrsToLabels(log, DefaultOTLPConfig(defaultGlobalOTLPConfig))
	require.NoError(t, err)
	assert.Equal(t, "otlp.field", labelAdapterGet(got.StructuredMetadata, OTLPEventName))
}

func TestLogAttrsToLabels_EventNameOnly(t *testing.T) {
	log := plog.NewLogRecord()
	log.Body().SetStr("log body")
	log.SetTimestamp(pcommon.Timestamp(1000000000))
	log.SetEventName("session.start")

	got, err := LogAttrsToLabels(log, DefaultOTLPConfig(defaultGlobalOTLPConfig))
	require.NoError(t, err)
	assert.Equal(t, "session.start", labelAdapterGet(got.StructuredMetadata, OTLPEventName))
}

func TestLogAttrsToLabels_SeverityTextAsIndexLabel(t *testing.T) {
	cfg := DefaultOTLPConfig(defaultGlobalOTLPConfig)
	cfg.SeverityTextAsLabel = true

	log := plog.NewLogRecord()
	log.Body().SetStr("log body")
	log.SetTimestamp(pcommon.Timestamp(1000000000))
	log.SetSeverityText("ERROR")

	got, err := LogAttrsToLabels(log, cfg)
	require.NoError(t, err)
	assert.Equal(t, "ERROR", labelSetGet(got.IndexLabels, OTLPSeverityText))
	assert.Equal(t, "ERROR", labelAdapterGet(got.StructuredMetadata, OTLPSeverityText))
}

func TestLogAttrsToLabels_IndexedLogAttributes(t *testing.T) {
	cfg := OTLPConfig{
		LogAttributes: []AttributesConfig{
			{Action: IndexLabel, Attributes: []string{"detected_level"}},
			{Action: StructuredMetadata, Attributes: []string{"trace_id"}},
			{Action: Drop, Regex: relabel.MustNewRegexp(".*")},
		},
	}

	log := plog.NewLogRecord()
	log.Body().SetStr("test")
	log.SetTimestamp(pcommon.Timestamp(1000000000))
	log.Attributes().PutStr("detected_level", "info")
	log.Attributes().PutStr("trace_id", "abc123")
	log.Attributes().PutStr("dropped", "gone")

	got, err := LogAttrsToLabels(log, cfg)
	require.NoError(t, err)
	assert.Equal(t, "info", labelSetGet(got.IndexLabels, "detected_level"))
	assert.Equal(t, "abc123", labelAdapterGet(got.StructuredMetadata, "trace_id"))
	assert.Equal(t, "", labelAdapterGet(got.StructuredMetadata, "dropped"))
	assert.Equal(t, "", labelSetGet(got.IndexLabels, "dropped"))
}

func TestLogAttrsToLabels_DroppedAttrsExcluded(t *testing.T) {
	cfg := OTLPConfig{
		LogAttributes: []AttributesConfig{
			{Action: Drop, Attributes: []string{"secret"}},
		},
	}

	log := plog.NewLogRecord()
	log.Body().SetStr("test")
	log.SetTimestamp(pcommon.Timestamp(1000000000))
	log.Attributes().PutStr("secret", "hidden")
	log.Attributes().PutStr("visible", "here")

	got, err := LogAttrsToLabels(log, cfg)
	require.NoError(t, err)
	assert.Equal(t, "", labelAdapterGet(got.StructuredMetadata, "secret"))
	assert.Equal(t, "here", labelAdapterGet(got.StructuredMetadata, "visible"))
}

func TestLogAttrsToLabels_ObservedTimestampOmittedWhenZero(t *testing.T) {
	log := plog.NewLogRecord()
	log.Body().SetStr("test")
	log.SetTimestamp(pcommon.Timestamp(1000000000))

	got, err := LogAttrsToLabels(log, DefaultOTLPConfig(defaultGlobalOTLPConfig))
	require.NoError(t, err)
	assert.Equal(t, "", labelAdapterGet(got.StructuredMetadata, "observed_timestamp"))
}

package otlplabels

import (
	"encoding/base64"
	"testing"

	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pcommon"

	"github.com/grafana/loki/pkg/push"
)

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
				{
					Name: "empty",
				},
				{
					Name:  "str",
					Value: "val",
				},
				{
					Name:  "int",
					Value: "1",
				},
				{
					Name:  "double",
					Value: "3.14",
				},
				{
					Name:  "bool",
					Value: "true",
				},
				{
					Name:  "bytes",
					Value: base64.StdEncoding.EncodeToString([]byte{1, 2, 3}),
				},
				{
					Name:  "slice",
					Value: `[1,["foo"],{"fizz":"buzz"}]`,
				},
				{
					Name:  "nested_foo",
					Value: "bar",
				},
				{
					Name:  "nested_more_key",
					Value: "val",
				},
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
				{
					Name:  "st_r",
					Value: "val",
				},
				{
					Name:  "nest_ed_fo_o",
					Value: "bar",
				},
				{
					Name:  "nest_ed_m_ore_k_ey",
					Value: "val",
				},
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

func TestResourceAttrsToStreamLabels_DiscoverServiceNameOrder(t *testing.T) {
	indexAll := OTLPConfig{
		ResourceAttributes: ResourceAttributesConfig{
			AttributesConfig: []AttributesConfig{
				{Action: IndexLabel, Regex: mustRegexp(".*")},
			},
		},
	}

	for _, tc := range []struct {
		name                string
		buildAttrs          func() pcommon.Map
		discoverServiceName []string
		expectedServiceName string
	}{
		{
			// container is inserted before app_kubernetes_io_name, but the configured
			// order lists app_kubernetes_io_name first, so it must win. See issue #22010.
			name: "configured order wins over attribute insertion order",
			buildAttrs: func() pcommon.Map {
				attrs := pcommon.NewMap()
				attrs.PutStr("container", "checkout")
				attrs.PutStr("app_kubernetes_io_name", "checkout-test")
				return attrs
			},
			discoverServiceName: []string{"app_kubernetes_io_name", "container"},
			expectedServiceName: "checkout-test",
		},
		{
			// Same attributes, reversed insertion order, must give the same result.
			name: "configured order wins regardless of attribute insertion order",
			buildAttrs: func() pcommon.Map {
				attrs := pcommon.NewMap()
				attrs.PutStr("app_kubernetes_io_name", "checkout-test")
				attrs.PutStr("container", "checkout")
				return attrs
			},
			discoverServiceName: []string{"app_kubernetes_io_name", "container"},
			expectedServiceName: "checkout-test",
		},
		{
			name: "falls back to next configured label when first is absent",
			buildAttrs: func() pcommon.Map {
				attrs := pcommon.NewMap()
				attrs.PutStr("container", "checkout")
				return attrs
			},
			discoverServiceName: []string{"app_kubernetes_io_name", "container"},
			expectedServiceName: "checkout",
		},
		{
			name: "defaults to unknown_service when no configured label is present",
			buildAttrs: func() pcommon.Map {
				attrs := pcommon.NewMap()
				attrs.PutStr("foo", "bar")
				return attrs
			},
			discoverServiceName: []string{"app_kubernetes_io_name", "container"},
			expectedServiceName: ServiceUnknown,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ResourceAttrsToStreamLabels(tc.buildAttrs(), indexAll, tc.discoverServiceName)
			require.NoError(t, err)
			require.Equal(t, tc.expectedServiceName, string(result.StreamLabels[LabelServiceName]))
		})
	}
}

func mustRegexp(s string) relabel.Regexp {
	re, err := relabel.NewRegexp(s)
	if err != nil {
		panic(err)
	}
	return re
}

package otlplabels

import (
	"encoding/base64"
	"testing"

	"github.com/prometheus/common/model"
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

func TestResourceAttrsToStreamLabels_ServiceNameDiscovery(t *testing.T) {
	type resourceAttribute struct {
		key   string
		value string
	}

	buildAttrs := func(attributes ...resourceAttribute) pcommon.Map {
		attrs := pcommon.NewMap()
		for _, attribute := range attributes {
			attrs.PutStr(attribute.key, attribute.value)
		}

		return attrs
	}

	otlpConfig := OTLPConfig{
		ResourceAttributes: ResourceAttributesConfig{
			IgnoreDefaults: true,
			AttributesConfig: []AttributesConfig{
				{
					Action: IndexLabel,
					Attributes: []string{
						AttrServiceName,
						"container.name",
						"k8s.pod.name",
					},
				},
			},
		},
	}

	for _, tc := range []struct {
		name                string
		discoverServiceName []string
		buildAttrs          func(iteration int) pcommon.Map
		expectedServiceName string
		iterations          int
	}{
		{
			name:                "service.name absent and only discovery candidate is empty",
			discoverServiceName: []string{"container_name"},
			buildAttrs: func(_ int) pcommon.Map {
				return buildAttrs(resourceAttribute{key: "container.name", value: ""})
			},
			expectedServiceName: ServiceUnknown,
		},
		{
			name:                "service.name absent and second discovery candidate is non-empty",
			discoverServiceName: []string{"container_name", "k8s_pod_name"},
			buildAttrs: func(_ int) pcommon.Map {
				return buildAttrs(
					resourceAttribute{key: "container.name", value: ""},
					resourceAttribute{key: "k8s.pod.name", value: "api-7d"},
				)
			},
			expectedServiceName: "api-7d",
		},
		{
			name:                "service.name empty and discovery candidates empty",
			discoverServiceName: []string{"container_name", "k8s_pod_name"},
			buildAttrs: func(_ int) pcommon.Map {
				return buildAttrs(
					resourceAttribute{key: AttrServiceName, value: ""},
					resourceAttribute{key: "container.name", value: ""},
					resourceAttribute{key: "k8s.pod.name", value: ""},
				)
			},
			expectedServiceName: ServiceUnknown,
		},
		{
			name:                "first configured discovery candidate wins regardless of attribute insertion order",
			discoverServiceName: []string{"container_name", "k8s_pod_name"},
			buildAttrs: func(iteration int) pcommon.Map {
				if iteration%2 == 0 {
					return buildAttrs(
						resourceAttribute{key: "container.name", value: "container-win"},
						resourceAttribute{key: "k8s.pod.name", value: "pod-second"},
					)
				}

				return buildAttrs(
					resourceAttribute{key: "k8s.pod.name", value: "pod-first"},
					resourceAttribute{key: "container.name", value: "container-win"},
				)
			},
			expectedServiceName: "container-win",
			iterations:          50,
		},
		{
			name:                "explicit service.name overrides discovery candidates",
			discoverServiceName: []string{"container_name", "k8s_pod_name"},
			buildAttrs: func(_ int) pcommon.Map {
				return buildAttrs(
					resourceAttribute{key: AttrServiceName, value: "svc"},
					resourceAttribute{key: "container.name", value: "container"},
					resourceAttribute{key: "k8s.pod.name", value: "pod"},
				)
			},
			expectedServiceName: "svc",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			iterations := tc.iterations
			if iterations == 0 {
				iterations = 1
			}

			for i := 0; i < iterations; i++ {
				result, err := ResourceAttrsToStreamLabels(tc.buildAttrs(i), otlpConfig, tc.discoverServiceName)
				require.NoError(t, err)
				require.Equal(t, tc.expectedServiceName, string(result.StreamLabels[model.LabelName(LabelServiceName)]))
			}
		})
	}
}

func TestStreamLabelsForRecord(t *testing.T) {
	resource := model.LabelSet{"service_name": "api", "env": "prod"}

	t.Run("no index labels returns the resource set itself", func(t *testing.T) {
		got, err := StreamLabelsForRecord(resource, nil)
		require.NoError(t, err)
		require.Equal(t, resource, got)
		got["env"] = "mutated"
		require.Equal(t, model.LabelValue("mutated"), resource["env"], "the same map is returned, not a copy")
		resource["env"] = "prod"

		got, err = StreamLabelsForRecord(resource, model.LabelSet{})
		require.NoError(t, err)
		require.Equal(t, resource, got)
	})

	t.Run("index labels are added and win on collision", func(t *testing.T) {
		got, err := StreamLabelsForRecord(resource, model.LabelSet{"env": "canary", "team": "blue"})
		require.NoError(t, err)
		require.Equal(t, model.LabelSet{"service_name": "api", "env": "canary", "team": "blue"}, got)
		require.Equal(t, model.LabelSet{"service_name": "api", "env": "prod"}, resource, "resource set is not modified")
	})

	t.Run("invalid combined set is an error", func(t *testing.T) {
		_, err := StreamLabelsForRecord(resource, model.LabelSet{"": "x"})
		require.ErrorContains(t, err, "invalid labels with log attributes")
	})
}

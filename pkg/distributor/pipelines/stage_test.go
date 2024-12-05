package pipelines

import (
	"context"
	"sort"
	"testing"

	"github.com/grafana/loki/v3/pkg/distributor/model"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/stretchr/testify/require"
)

func newContainer(s logproto.Stream) model.KeyedStream {
	lbs, err := syntax.ParseLabels(s.Labels)
	if err != nil {
		panic("invalid labels")
	}

	sort.Sort(lbs)
	s.Labels, s.Hash = lbs.String(), lbs.Hash()

	return model.KeyedStream{
		Stream:       s,
		ParsedLabels: logproto.FromLabelsToLabelAdapters(lbs),
	}
}

func TestCompileStage(t *testing.T) {
	stages := []Stage{
		{Action: "parse_logfmt"},
		{Action: "parse_json"},
		{Action: "parse_pattern", Config: map[string]string{"pattern": "<_> cluster=<cluster> <_>"}},
		{Action: "drop_label"},
		{Action: "drop_metadata"},
		{Action: "promote_metadata_to_label"},
		{Action: "promote_field_to_label"},
		{Action: "degrade_label_to_metadata"},
	}

	for _, st := range stages {
		t.Run(string(st.Action), func(t *testing.T) {
			_, err := st.Compile()
			require.NoError(t, err)
		})
	}

	t.Run("invalid action", func(t *testing.T) {
		st := Stage{Action: "does_not_exist"}
		_, err := st.Compile()
		require.Error(t, err)
	})
}

func TestStage_DropLabel(t *testing.T) {
	inp := []model.KeyedStream{
		newContainer(logproto.Stream{
			Labels: `{cluster="prod", namespace="loki", pod="ingester-0"}`,
			Entries: []logproto.Entry{
				{Line: "line stream 1"},
			},
		}),
		newContainer(logproto.Stream{
			Labels: `{cluster="prod", namespace="loki", container="ingester"}`,
			Entries: []logproto.Entry{
				{Line: "line stream 2"},
			},
		}),
	}
	st := Stage{
		Action: "drop_label",
		Config: map[string]string{
			"pod":       "",
			"container": "",
		},
	}
	exp := []model.KeyedStream{
		newContainer(logproto.Stream{
			Labels: `{cluster="prod", namespace="loki"}`,
			Entries: []logproto.Entry{
				{Line: "line stream 1"},
				{Line: "line stream 2"},
			},
		}),
	}

	p, err := st.Compile()
	require.NoError(t, err)

	streams := model.NewStreamsBuilder(0, inp...)
	err = p.Apply(context.TODO(), streams)
	require.NoError(t, err)

	requireEqualStreams(t, exp, streams.Build())
}

func TestStage_DropMetadata(t *testing.T) {
	inp := []model.KeyedStream{
		newContainer(logproto.Stream{
			Labels: `{cluster="prod", namespace="loki"}`,
			Entries: []logproto.Entry{
				{
					Line: "line 1",
					StructuredMetadata: logproto.LabelsAdapter{
						{Name: "pod", Value: "ingester-0"},
						{Name: "trace_id", Value: "cdde57d74c43e853"},
					},
				},
				{
					Line: "line 2",
					StructuredMetadata: logproto.LabelsAdapter{
						{Name: "pod", Value: "ingester-0"},
					},
				},
				{
					Line: "line 3",
					StructuredMetadata: logproto.LabelsAdapter{
						{Name: "pod", Value: "ingester-0"},
						{Name: "traceID", Value: "b8a9d74459405fe3"},
					},
				},
			},
		}),
	}
	st := Stage{
		Action: "drop_metadata",
		Config: map[string]string{
			"trace_id": "",
			"traceID":  "",
		},
	}
	exp := []model.KeyedStream{
		newContainer(logproto.Stream{
			Labels: `{cluster="prod", namespace="loki"}`,
			Entries: []logproto.Entry{
				{
					Line: "line 1",
					StructuredMetadata: logproto.LabelsAdapter{
						{Name: "pod", Value: "ingester-0"},
					},
				},
				{
					Line: "line 2",
					StructuredMetadata: logproto.LabelsAdapter{
						{Name: "pod", Value: "ingester-0"},
					},
				},
				{
					Line: "line 3",
					StructuredMetadata: logproto.LabelsAdapter{
						{Name: "pod", Value: "ingester-0"},
					},
				},
			},
		}),
	}

	p, err := st.Compile()
	require.NoError(t, err)

	streams := model.NewStreamsBuilder(0, inp...)
	err = p.Apply(context.TODO(), streams)
	require.NoError(t, err)

	requireEqualStreams(t, exp, streams.Build())
}

func TestStage_PromoteMetadataToLabel(t *testing.T) {
	inp := []model.KeyedStream{
		newContainer(logproto.Stream{
			Labels: `{cluster="prod", namespace="loki"}`,
			Entries: []logproto.Entry{
				{
					Line: "stream 1 line 1",
					StructuredMetadata: logproto.LabelsAdapter{
						{Name: "pod", Value: "ingester-0"},
					},
				},
				{
					Line: "stream 1 line 2",
					StructuredMetadata: logproto.LabelsAdapter{
						{Name: "foo", Value: "bar"},
					},
				},
			},
		}),
		newContainer(logproto.Stream{
			Labels: `{cluster="prod"}`,
			Entries: []logproto.Entry{
				{
					Line: "stream 2 line 1",
					StructuredMetadata: logproto.LabelsAdapter{
						{Name: "pod", Value: "ingester-0"},
						{Name: "namespace", Value: "loki"},
					},
				},
				{
					Line: "stream 2 line 2",
					StructuredMetadata: logproto.LabelsAdapter{
						{Name: "pod", Value: "ingester-0"},
					},
				},
			},
		}),
	}
	st := Stage{
		Action: "promote_metadata_to_label",
		Config: map[string]string{
			"namespace": "environment",
			"pod":       "",
		},
	}
	exp := []model.KeyedStream{
		newContainer(logproto.Stream{
			Labels: `{cluster="prod", environment="loki", pod="ingester-0"}`,
			Entries: []logproto.Entry{
				{
					Line:               "stream 2 line 1",
					StructuredMetadata: logproto.LabelsAdapter{},
				},
			},
		}),
		newContainer(logproto.Stream{
			Labels: `{cluster="prod", namespace="loki"}`,
			Entries: []logproto.Entry{
				{
					Line: "stream 1 line 2",
					StructuredMetadata: logproto.LabelsAdapter{
						{Name: "foo", Value: "bar"},
					},
				},
			},
		}),
		newContainer(logproto.Stream{
			Labels: `{cluster="prod", namespace="loki", pod="ingester-0"}`,
			Entries: []logproto.Entry{
				{
					Line:               "stream 1 line 1",
					StructuredMetadata: logproto.LabelsAdapter{},
				},
			},
		}),
		newContainer(logproto.Stream{
			Labels: `{cluster="prod", pod="ingester-0"}`,
			Entries: []logproto.Entry{
				{
					Line:               "stream 2 line 2",
					StructuredMetadata: logproto.LabelsAdapter{},
				},
			},
		}),
	}

	p, err := st.Compile()
	require.NoError(t, err)

	streams := model.NewStreamsBuilder(0, inp...)
	err = p.Apply(context.TODO(), streams)
	require.NoError(t, err)

	requireEqualStreams(t, exp, streams.Build())
}

func TestStage_DegradeLabelToMetadata(t *testing.T) {
	inp := []model.KeyedStream{
		newContainer(logproto.Stream{
			Labels: `{cluster="prod", namespace="loki", pod="ingester-0"}`,
			Entries: []logproto.Entry{
				{Line: "stream 1 line 1"},
			},
		}),
		newContainer(logproto.Stream{
			Labels: `{cluster="prod", container="ingester", namespace="loki"}`,
			Entries: []logproto.Entry{
				{Line: "stream 2 line 1"},
			},
		}),
	}
	st := Stage{
		Action: "degrade_label_to_metadata",
		Config: map[string]string{
			"pod":       "pod_or_container",
			"container": "pod_or_container",
		},
	}
	exp := []model.KeyedStream{
		newContainer(logproto.Stream{
			Labels: `{cluster="prod", namespace="loki"}`,
			Entries: []logproto.Entry{
				{
					Line: "stream 1 line 1",
					StructuredMetadata: logproto.LabelsAdapter{
						{Name: "pod_or_container", Value: "ingester-0"},
					},
				},
				{
					Line: "stream 2 line 1",
					StructuredMetadata: logproto.LabelsAdapter{
						{Name: "pod_or_container", Value: "ingester"},
					},
				},
			},
		}),
	}

	p, err := st.Compile()
	require.NoError(t, err)

	streams := model.NewStreamsBuilder(0, inp...)
	err = p.Apply(context.TODO(), streams)
	require.NoError(t, err)

	requireEqualStreams(t, exp, streams.Build())
}

func requireEqualStreams(t *testing.T, expected, actual []model.KeyedStream) {
	sort.Slice(expected, func(i, j int) bool { return expected[i].Labels < expected[j].Labels })
	sort.Slice(actual, func(i, j int) bool { return actual[i].Labels < actual[j].Labels })
	require.Equal(t, len(expected), len(actual))
	for i := 0; i < len(expected); i++ {
		a, b := expected[i], actual[i]
		require.Equal(t, a.Labels, b.Labels)
		require.Equal(t, a.Hash, b.Hash)
		require.ElementsMatch(t, a.Entries, b.Entries)
	}
}

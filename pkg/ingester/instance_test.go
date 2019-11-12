package ingester

import (
	"context"
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logproto"

	"github.com/grafana/loki/pkg/util/validation"
	"github.com/stretchr/testify/require"
)

func TestLabelsCollisions(t *testing.T) {
	o, err := validation.NewOverrides(validation.Limits{MaxStreamsPerUser: 1000})
	require.NoError(t, err)

	i := newInstance("test", 512, o)

	tt := time.Now().Add(-5 * time.Minute)

	// Notice how labels aren't sorted.
	err = i.Push(context.Background(), &logproto.PushRequest{Streams: []*logproto.Stream{
		// both label sets have FastFingerprint=e002a3a451262627
		{Labels: "{app=\"l\",uniq0=\"0\",uniq1=\"1\"}", Entries: entries(tt.Add(time.Minute))},
		{Labels: "{uniq0=\"1\",app=\"m\",uniq1=\"1\"}", Entries: entries(tt)},

		// e002a3a451262247
		{Labels: "{app=\"l\",uniq0=\"1\",uniq1=\"0\"}", Entries: entries(tt.Add(time.Minute))},
		{Labels: "{uniq1=\"0\",app=\"m\",uniq0=\"0\"}", Entries: entries(tt)},

		// e002a2a4512624f4
		{Labels: "{app=\"l\",uniq0=\"0\",uniq1=\"0\"}", Entries: entries(tt.Add(time.Minute))},
		{Labels: "{uniq0=\"1\",uniq1=\"0\",app=\"m\"}", Entries: entries(tt)},
	}})
	require.NoError(t, err)
}

func entries(time time.Time) []logproto.Entry {
	return []logproto.Entry{{Timestamp: time, Line: "hello"}}
}

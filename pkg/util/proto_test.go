package util_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util"
)

func TestParseProto(t *testing.T) {
	req := &logproto.PushRequest{
		Streams: []logproto.Stream{
			{
				Labels:  `{foo="bar"}`,
				Entries: []logproto.Entry{{Timestamp: time.Unix(0, 1).UTC(), Line: "line1"}},
			},
		},
	}

	t.Run("valid proto", func(t *testing.T) {
		body, err := req.Marshal()
		require.NoError(t, err)

		var fromWire logproto.PushRequest
		err = util.ParseProto(body, &fromWire)
		assert.NoError(t, err)
		assert.Equal(t, req, &fromWire)
	})

	t.Run("invalid proto", func(t *testing.T) {
		var fromWire logproto.PushRequest
		err := util.ParseProto([]byte{0xff, 0xff, 0xff}, &fromWire)
		assert.Error(t, err)
	})
}

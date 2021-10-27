package client

import (
	"net/url"
	"testing"
	"time"

	util_log "github.com/cortexproject/cortex/pkg/util/log"
	cortexflag "github.com/grafana/dskit/flagext"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/clients/pkg/promtail/api"

	"github.com/grafana/loki/pkg/logproto"
)

func TestNewLogger(t *testing.T) {
	_, err := NewLogger(nil, util_log.Logger, []Config{}...)
	require.Error(t, err)

	l, err := NewLogger(nil, util_log.Logger, []Config{{URL: cortexflag.URLValue{URL: &url.URL{Host: "string"}}}}...)
	require.NoError(t, err)
	l.Chan() <- api.Entry{Labels: model.LabelSet{"foo": "bar"}, Entry: logproto.Entry{Timestamp: time.Now(), Line: "entry"}}
	l.Stop()
}

package client

import (
	"net/url"
	"testing"
	"time"

	cortexflag "github.com/cortexproject/cortex/pkg/util/flagext"
	util_log "github.com/cortexproject/cortex/pkg/util/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v2/pkg/logproto"
	"github.com/grafana/loki/v2/pkg/promtail/api"
	"github.com/grafana/loki/v2/pkg/util/flagext"
)

func TestNewLogger(t *testing.T) {
	_, err := NewLogger(nil, util_log.Logger, flagext.LabelSet{}, []Config{}...)
	require.Error(t, err)

	l, err := NewLogger(nil, util_log.Logger, flagext.LabelSet{}, []Config{{URL: cortexflag.URLValue{URL: &url.URL{Host: "string"}}}}...)
	require.NoError(t, err)
	l.Chan() <- api.Entry{Labels: model.LabelSet{"foo": "bar"}, Entry: logproto.Entry{Timestamp: time.Now(), Line: "entry"}}
	l.Stop()
}

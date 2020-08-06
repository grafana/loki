package client

import (
	"net/url"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	cortexflag "github.com/cortexproject/cortex/pkg/util/flagext"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/pkg/util/flagext"
)

func TestNewLogger(t *testing.T) {
	_, err := NewLogger(util.Logger, flagext.LabelSet{}, []Config{}...)
	require.Error(t, err)

	l, err := NewLogger(util.Logger, flagext.LabelSet{}, []Config{{URL: cortexflag.URLValue{URL: &url.URL{Host: "string"}}}}...)
	require.NoError(t, err)
	err = l.Handle(model.LabelSet{"foo": "bar"}, time.Now(), "entry")
	require.NoError(t, err)
}

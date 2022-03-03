package gelf

import (
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"
	"gopkg.in/Graylog2/go-gelf.v2/gelf"

	"github.com/grafana/loki/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
)

func Test_Gelf(t *testing.T) {
	client := fake.New(func() {})

	tm, err := NewTargetManager(NewMetrics(nil), log.NewNopLogger(), client, []scrapeconfig.Config{
		{
			JobName: "gelf",
			GelfConfig: &scrapeconfig.GelfTargetConfig{
				ListenAddress:        ":12201",
				UseIncomingTimestamp: true,
				Labels:               model.LabelSet{"cfg": "true"},
			},
			RelabelConfigs: []*relabel.Config{
				{
					SourceLabels: model.LabelNames{"__gelf_message_level"},
					TargetLabel:  "level",
					Replacement:  "$1",
					Action:       relabel.Replace,
					Regex:        relabel.MustNewRegexp("(.*)"),
				},
				{
					SourceLabels: model.LabelNames{"__gelf_message_host"},
					TargetLabel:  "hostname",
					Replacement:  "$1",
					Action:       relabel.Replace,
					Regex:        relabel.MustNewRegexp("(.*)"),
				},
				{
					SourceLabels: model.LabelNames{"__gelf_message_facility"},
					TargetLabel:  "facility",
					Replacement:  "$1",
					Action:       relabel.Replace,
					Regex:        relabel.MustNewRegexp("(.*)"),
				},
			},
		},
	})
	require.NoError(t, err)

	w, err := gelf.NewUDPWriter(":12201")
	require.NoError(t, err)
	baseTs := float64(time.Unix(10, 0).Unix()) + 0.250
	ts := baseTs

	for i := 0; i < 10; i++ {
		require.NoError(t, w.WriteMessage(&gelf.Message{
			Short:    "short",
			Full:     "full",
			Version:  "2.2",
			Host:     "thishost",
			TimeUnix: ts,
			Level:    gelf.LOG_ERR,
			Facility: "gelftest",
			Extra: map[string]interface{}{
				"_foo": "bar",
			},
		}))
		ts += 0.250
	}

	require.Eventually(t, func() bool {
		return len(client.Received()) == 10
	}, 200*time.Millisecond, 20*time.Millisecond)

	for i, actual := range client.Received() {
		require.Equal(t, "error", string(actual.Labels["level"]))
		require.Equal(t, "true", string(actual.Labels["cfg"]))
		require.Equal(t, "thishost", string(actual.Labels["hostname"]))
		require.Equal(t, "gelftest", string(actual.Labels["facility"]))

		require.Equal(t, secondsToUnixTimestamp(baseTs+float64(i)*0.250), actual.Timestamp)

		var gelfMsg gelf.Message

		require.NoError(t, gelfMsg.UnmarshalJSON([]byte(actual.Line)))

		require.Equal(t, "short", gelfMsg.Short)
		require.Equal(t, "full", gelfMsg.Full)
		require.Equal(t, "2.2", gelfMsg.Version)
		require.Equal(t, "thishost", gelfMsg.Host)
		require.Equal(t, "bar", gelfMsg.Extra["_foo"])
		require.Equal(t, gelf.LOG_ERR, int(gelfMsg.Level))
		require.Equal(t, "gelftest", gelfMsg.Facility)

	}

	tm.Stop()
}

func TestConvertTime(t *testing.T) {
	require.Equal(t, time.Unix(0, int64(time.Second+(time.Duration(250)*time.Millisecond))), secondsToUnixTimestamp(float64(time.Unix(1, 0).Unix())+0.250))
}

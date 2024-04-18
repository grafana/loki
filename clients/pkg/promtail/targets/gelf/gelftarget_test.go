package gelf

import (
	"crypto/rand"
	"fmt"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/go-gelf/v2/gelf"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/clients/pkg/promtail/client/fake"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
)

func Test_Gelf(t *testing.T) {
	client := fake.New(func() {})
	tm, err := NewTargetManager(NewMetrics(nil), log.NewNopLogger(), client, []scrapeconfig.Config{
		{
			JobName: "gelf",
			GelfConfig: &scrapeconfig.GelfTargetConfig{
				ListenAddress:        ":0",
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
	defer tm.Stop()
	target := tm.targets["gelf"]
	require.NotNil(t, target)
	w, err := gelf.NewUDPWriter(target.gelfReader.Addr())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, w.Close())
	}()
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
	}, 1*time.Second, 20*time.Millisecond)

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
}

func TestConvertTime(t *testing.T) {
	require.Equal(t, time.Unix(0, int64(time.Second+(time.Duration(250)*time.Millisecond))), secondsToUnixTimestamp(float64(time.Unix(1, 0).Unix())+0.250))
}

func Test_GelfChunksUnordered(t *testing.T) {
	client := fake.New(func() {})

	tm, err := NewTargetManager(NewMetrics(nil), log.NewNopLogger(), client, []scrapeconfig.Config{
		{
			JobName: "gelf",
			GelfConfig: &scrapeconfig.GelfTargetConfig{
				ListenAddress: ":0",
			},
		},
	})
	require.NoError(t, err)
	defer tm.Stop()

	target := tm.targets["gelf"]
	require.NotNil(t, target)
	connection, err := net.Dial("udp", target.gelfReader.Addr())
	require.NoError(t, err)
	defer func() {
		require.NoError(t, connection.Close())
	}()

	chunksA := createChunks(t, "a")
	chunksB := createChunks(t, "b")
	// send messages(a, b) chunks in order: chunk-0a, chunk-0b, chunk-1a, chunk-1b
	for i := 0; i < len(chunksB); i++ {
		writeA, err := connection.Write(chunksA[i])
		require.NoError(t, err)
		require.Equal(t, len(chunksA[i]), writeA)

		writeB, err := connection.Write(chunksB[i])
		require.NoError(t, err)
		require.Equal(t, len(chunksB[i]), writeB)
	}

	require.Eventually(t, func() bool {
		return len(client.Received()) == 2
	}, 2*time.Second, 100*time.Millisecond, "expected 2 messages to be received")
}

func createChunks(t *testing.T, char string) [][]byte {
	chunksA, err := splitToChunks([]byte(fmt.Sprintf("{\"short_message\":\"%v\"}", strings.Repeat(char, gelf.ChunkSize*2))))
	require.NoError(t, err)
	return chunksA
}

// static value that indicated that GELF message is chunked
var magicChunked = []byte{0x1e, 0x0f}

const (
	chunkedHeaderLen = 12
	chunkedDataLen   = gelf.ChunkSize - chunkedHeaderLen
)

func splitToChunks(messageBytes []byte) ([][]byte, error) {
	chunksCount := uint8(len(messageBytes)/chunkedDataLen + 1)
	messageID := make([]byte, 8)
	n, err := io.ReadFull(rand.Reader, messageID)
	if err != nil || n != 8 {
		return nil, fmt.Errorf("rand.Reader: %d/%s", n, err)
	}
	chunks := make([][]byte, 0, chunksCount)
	bytesLeft := len(messageBytes)
	for i := uint8(0); i < chunksCount; i++ {
		buf := make([]byte, 0, gelf.ChunkSize)
		buf = append(buf, magicChunked...)
		buf = append(buf, messageID...)
		buf = append(buf, i)
		buf = append(buf, chunksCount)
		chunkLen := chunkedDataLen
		if chunkLen > bytesLeft {
			chunkLen = bytesLeft
		}
		off := int(i) * chunkedDataLen
		chunkData := messageBytes[off : off+chunkLen]
		buf = append(buf, chunkData...)

		chunks = append(chunks, buf)
		bytesLeft -= chunkLen
	}

	if bytesLeft != 0 {
		return nil, fmt.Errorf("error: %d bytes left after splitting", bytesLeft)
	}
	return chunks, nil
}

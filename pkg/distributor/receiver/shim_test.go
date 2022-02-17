package receiver

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/weaveworks/common/logging"
	"go.opentelemetry.io/collector/model/otlpgrpc"
	"go.opentelemetry.io/collector/model/pdata"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/grafana/loki/pkg/logproto"
)

func TestParseEntry(t *testing.T) {
	pLog := pdata.NewLogRecord()
	pLog.SetName("testName")
	pLog.SetFlags(31)
	pLog.SetSeverityNumber(1)
	pLog.SetSeverityText("WARN LEVEL")
	pLog.SetSpanID(pdata.NewSpanID([8]byte{1, 2}))
	pLog.SetTraceID(pdata.NewTraceID([16]byte{1, 2, 3, 4}))

	now := time.Now()
	pLog.SetTimestamp(pdata.NewTimestampFromTime(now))

	entry, err := parseEntry(pLog, "json")
	if err != nil {
		t.Fatal(err)
	}
	expexted := logproto.Entry{
		Timestamp: now,
		Line:      `{"severity_number":1,"severity_text":"WARN LEVEL","name":"testName","body":"","flags":31,"trace_id":"01020304000000000000000000000000","span_id":"0102000000000000"}`,
	}
	require.Equal(t, expexted.Line, (*entry).Line)
	require.Equal(t, expexted.Timestamp.UnixMilli(), (*entry).Timestamp.UnixMilli())

	entry, err = parseEntry(pLog, "logfmt")
	if err != nil {
		t.Fatal(err)
	}
	expextedLogfmt := logproto.Entry{
		Timestamp: now,
		Line:      `body= flags=31 name=testName severity_number=1 severity_text="WARN LEVEL" span_id=0102000000000000 trace_id=01020304000000000000000000000000`,
	}
	require.Equal(t, expextedLogfmt.Line, (*entry).Line)

}

func TestParseLabel(t *testing.T) {
	resource := pdata.NewAttributeMap()
	resource.Insert("app", pdata.NewAttributeValueString("testApp"))
	attr := pdata.NewAttributeMap()
	attr.Insert("level", pdata.NewAttributeValueString("ERROR"))
	labels := parseLabel(resource, attr)

	expexted := `{app="testApp", level="ERROR"}`
	require.Equal(t, expexted, labels)
}

func TestParseLog(t *testing.T) {
	pLog := pdata.NewLogs()

	pmm := pLog.ResourceLogs().AppendEmpty()
	pmm.Resource().Attributes().InsertString("app", "testApp")

	ilm := pmm.InstrumentationLibraryLogs().AppendEmpty()
	ilm.InstrumentationLibrary().SetName("testName")

	now := time.Now()

	logReocrd := ilm.LogRecords().AppendEmpty()
	logReocrd.SetName("testName")
	logReocrd.SetFlags(31)
	logReocrd.SetSeverityNumber(1)
	logReocrd.SetSeverityText("WARN")
	logReocrd.SetSpanID(pdata.NewSpanID([8]byte{1, 2}))
	logReocrd.SetTraceID(pdata.NewTraceID([16]byte{1, 2, 3, 4}))
	logReocrd.Attributes().InsertString("level", "WARN")
	logReocrd.SetTimestamp(pdata.NewTimestampFromTime(now))

	logReocrd2 := ilm.LogRecords().AppendEmpty()
	logReocrd2.SetName("testName")
	logReocrd2.SetFlags(31)
	logReocrd2.SetSeverityNumber(1)
	logReocrd2.SetSeverityText("INFO")
	logReocrd2.SetSpanID(pdata.NewSpanID([8]byte{3, 4}))
	logReocrd2.SetTraceID(pdata.NewTraceID([16]byte{1, 2, 3, 4}))
	logReocrd2.Attributes().InsertString("level", "WARN")
	logReocrd2.SetTimestamp(pdata.NewTimestampFromTime(now))

	res, err := parseLog(pLog, "json")
	if err != nil {
		t.Fatal(err)
	}
	expexted := logproto.PushRequest{
		Streams: []logproto.Stream{
			{Labels: "{app=\"testApp\", level=\"WARN\"}", Entries: []logproto.Entry{
				{Timestamp: now, Line: `{"severity_number":1,"severity_text":"WARN","name":"testName","body":"","flags":31,"trace_id":"01020304000000000000000000000000","span_id":"0102000000000000"}`},
				{Timestamp: now, Line: `{"severity_number":1,"severity_text":"INFO","name":"testName","body":"","flags":31,"trace_id":"01020304000000000000000000000000","span_id":"0304000000000000"}`},
			}},
		},
	}
	//
	require.Equal(t, len(expexted.Streams), len((*res).Streams))
	// test label
	require.Equal(t, expexted.Streams[0].Labels, (*res).Streams[0].Labels)
	// test entry
	require.Equal(t, expexted.Streams[0].Entries[0].Line, (*res).Streams[0].Entries[0].Line)
	require.Equal(t, expexted.Streams[0].Entries[0].Timestamp.UnixMilli(), (*res).Streams[0].Entries[0].Timestamp.UnixMilli())
	require.Equal(t, expexted.Streams[0].Entries[1].Line, (*res).Streams[0].Entries[1].Line)
	require.Equal(t, expexted.Streams[0].Entries[1].Timestamp.UnixMilli(), (*res).Streams[0].Entries[1].Timestamp.UnixMilli())

}

func TestOtlpPush(t *testing.T) {
	grpcEndpoint := "localhost:4317"
	var defaultReceivers = map[string]interface{}{
		"otlp": map[string]interface{}{
			"protocols": map[string]interface{}{
				"grpc": map[string]interface{}{
					"endpoint": grpcEndpoint,
				},
				"http": map[string]interface{}{
					"endpoint": "localhost:4318",
				},
			},
		},
	}
	level := logging.Level{}
	err := level.Set("debug")
	require.NoError(t, err)
	morkPusher := &morkPusher{}
	service, err := New(defaultReceivers, morkPusher, "json", time.Second, FakeTenantMiddleware(), level)
	require.NoError(t, err)
	require.NoError(t, service.StartAsync(context.Background()))
	require.NoError(t, service.AwaitRunning(context.Background()))
	defer func() {
		service.StopAsync()
		require.NoError(t, service.AwaitTerminated(context.Background()))
	}()
	//client
	addr := grpcEndpoint
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	client := otlpgrpc.NewLogsClient(conn)
	request := markRequest()
	_, err = client.Export(context.Background(), request)
	require.NoError(t, err)
	require.Equal(t, 1, len(morkPusher.Data))
	require.Equal(t, 1, len(morkPusher.Data[0].Streams))
	require.Equal(t, "{app=\"testApp\", level=\"WARN\"}", morkPusher.Data[0].Streams[0].Labels)
	require.Equal(t, "{\"severity_number\":1,\"severity_text\":\"WARN\",\"name\":\"testName\",\"body\":\"\",\"flags\":31,\"trace_id\":\"01020304000000000000000000000000\",\"span_id\":\"0102000000000000\"}", morkPusher.Data[0].Streams[0].Entries[0].Line)
}

func markRequest() otlpgrpc.LogsRequest {
	request := otlpgrpc.NewLogsRequest()
	pLog := pdata.NewLogs()

	pmm := pLog.ResourceLogs().AppendEmpty()
	pmm.Resource().Attributes().InsertString("app", "testApp")

	ilm := pmm.InstrumentationLibraryLogs().AppendEmpty()
	ilm.InstrumentationLibrary().SetName("testName")

	now := time.Now()

	logReocrd := ilm.LogRecords().AppendEmpty()
	logReocrd.SetName("testName")
	logReocrd.SetFlags(31)
	logReocrd.SetSeverityNumber(1)
	logReocrd.SetSeverityText("WARN")
	logReocrd.SetSpanID(pdata.NewSpanID([8]byte{1, 2}))
	logReocrd.SetTraceID(pdata.NewTraceID([16]byte{1, 2, 3, 4}))
	logReocrd.Attributes().InsertString("level", "WARN")
	logReocrd.SetTimestamp(pdata.NewTimestampFromTime(now))

	logReocrd2 := ilm.LogRecords().AppendEmpty()
	logReocrd2.SetName("testName")
	logReocrd2.SetFlags(31)
	logReocrd2.SetSeverityNumber(1)
	logReocrd2.SetSeverityText("INFO")
	logReocrd2.SetSpanID(pdata.NewSpanID([8]byte{3, 4}))
	logReocrd2.SetTraceID(pdata.NewTraceID([16]byte{1, 2, 3, 4}))
	logReocrd2.Attributes().InsertString("level", "WARN")
	logReocrd2.SetTimestamp(pdata.NewTimestampFromTime(now))
	request.SetLogs(pLog)
	return request
}

type mockPusher struct {
	Data []*logproto.PushRequest
}

func (m *morkPusher) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	m.Data = append(m.Data, req)
	return nil, nil
}

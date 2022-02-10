package receiver

import (
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/model/pdata"
)

func TestParseEntry(t *testing.T) {
	pLog := pdata.NewLogRecord()
	pLog.SetName("testName")
	pLog.SetFlags(31)
	pLog.SetSeverityNumber(1)
	pLog.SetSeverityText("WARN")
	pLog.SetSpanID(pdata.NewSpanID([8]byte{1, 2}))
	pLog.SetTraceID(pdata.NewTraceID([16]byte{1, 2, 3, 4}))

	now := time.Now()
	pLog.SetTimestamp(pdata.NewTimestampFromTime(now))

	entry, err := parseEntry(pLog)
	if err != nil {
		t.Fatal(err)
	}
	expexted := logproto.Entry{
		Timestamp: now,
		Line:      `{"severity_number":1,"severity_text":"WARN","name":"testName","body":"","flags":31,"trace_id":"01020304000000000000000000000000","span_id":"0102000000000000"}`,
	}
	require.Equal(t, expexted.Line, (*entry).Line)
	require.Equal(t, expexted.Timestamp.UnixMilli(), (*entry).Timestamp.UnixMilli())
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

	logReocrd := ilm.Logs().AppendEmpty()
	logReocrd.SetName("testName")
	logReocrd.SetFlags(31)
	logReocrd.SetSeverityNumber(1)
	logReocrd.SetSeverityText("WARN")
	logReocrd.SetSpanID(pdata.NewSpanID([8]byte{1, 2}))
	logReocrd.SetTraceID(pdata.NewTraceID([16]byte{1, 2, 3, 4}))
	logReocrd.Attributes().InsertString("level", "WARN")
	logReocrd.SetTimestamp(pdata.NewTimestampFromTime(now))

	logReocrd2 := ilm.Logs().AppendEmpty()
	logReocrd2.SetName("testName")
	logReocrd2.SetFlags(31)
	logReocrd2.SetSeverityNumber(1)
	logReocrd2.SetSeverityText("INFO")
	logReocrd2.SetSpanID(pdata.NewSpanID([8]byte{3, 4}))
	logReocrd2.SetTraceID(pdata.NewTraceID([16]byte{1, 2, 3, 4}))
	logReocrd2.Attributes().InsertString("level", "WARN")
	logReocrd2.SetTimestamp(pdata.NewTimestampFromTime(now))

	res, err := parseLog(pLog)
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

package otlp

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.opentelemetry.io/collector/pdata/plog/plogotlp"

	"github.com/grafana/loki/pkg/logproto"
)

func TestExport(t *testing.T) {
	// prepare
	logs := plog.NewLogs()
	logs.ResourceLogs().AppendEmpty()
	logs.ResourceLogs().At(0).Resource().Attributes().PutString("host.name", "guarana")
	logs.ResourceLogs().At(0).Resource().Attributes().PutString("cloud.region", "eu-west-1")
	logs.ResourceLogs().At(0).ScopeLogs().AppendEmpty()

	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().AppendEmpty()
	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Body().SetStr("operation succeeded")
	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutString("loki.resource.labels", "host.name,cloud.region")
	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutInt("http.status_code", 200)
	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(0).Attributes().PutString("http.method", "GET")

	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().AppendEmpty()
	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(1).Body().SetStr("page not found")
	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(1).Attributes().PutString("loki.resource.labels", "host.name,cloud.region")
	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(1).Attributes().PutInt("http.status_code", 404)
	logs.ResourceLogs().At(0).ScopeLogs().At(0).LogRecords().At(1).Attributes().PutString("http.method", "POST")

	req := plogotlp.NewRequestFromLogs(logs)
	pusher := &mockPusher{}
	srv := NewServer(pusher)

	// test
	_, err := srv.Export(context.Background(), req)
	require.NoError(t, err)

	// verify
	assert.Len(t, pusher.req.Streams, 1)
	assert.Len(t, pusher.req.Streams[0].Entries, 2)

	// this *might* make the test flaky, move it to the commented out section
	// if failures are observed without apparent cause
	assert.Equal(t, pusher.req.Streams[0].Labels, `{cloud.region="eu-west-1", exporter="OTLP", host.name="guarana"}`)

	// commented out as this has the potential for making the tests flaky
	// the correctness of the translation is asserted by the translation package
	// from the OTel Collector contrib repository.
	// assert.Equal(t, pusher.req.Streams[0].Entries[0].Line, `{"body":"operation succeeded","attributes":{"http.method":"GET","http.status_code":200}}`)
	// assert.Equal(t, pusher.req.Streams[0].Entries[1].Line, `{"body":"page not found","attributes":{"http.method":"POST","http.status_code":404}}`)
}

type mockPusher struct {
	logproto.UnimplementedPusherServer
	req *logproto.PushRequest
}

func (m *mockPusher) Push(ctx context.Context, req *logproto.PushRequest) (*logproto.PushResponse, error) {
	m.req = req
	return nil, nil
}

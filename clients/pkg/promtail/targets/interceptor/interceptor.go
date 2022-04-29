package interceptor

import (
	"github.com/grafana/loki/clients/pkg/logentry/stages"
	"github.com/grafana/loki/clients/pkg/promtail/api"
)

type Interceptor struct {
	Client      api.EntryHandler
	PipelineIn  api.EntryHandler
	PipelineOut chan stages.Entry
}

func NewInterceptor(client, pipelineIn api.EntryHandler, pipelineOut chan stages.Entry) Interceptor {
	return Interceptor{
		Client:      client,
		PipelineIn:  pipelineIn,
		PipelineOut: pipelineOut,
	}
}

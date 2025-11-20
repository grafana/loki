package types

import lokipush "github.com/grafana/loki/pkg/push"

type (
	PushRequest   = lokipush.PushRequest
	PushResponse  = lokipush.PushResponse
	Stream        = lokipush.Stream
	Entry         = lokipush.Entry
	LabelAdapter  = lokipush.LabelAdapter
	LabelsAdapter = lokipush.LabelsAdapter
)

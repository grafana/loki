package push

import (
	"github.com/grafana/loki/v3/pkg/loghttp/push/otlplabels"
)

// Type aliases for backward compatibility. All definitions live in otlplabels.
type Action = otlplabels.Action
type OTLPConfig = otlplabels.OTLPConfig
type GlobalOTLPConfig = otlplabels.GlobalOTLPConfig
type AttributesConfig = otlplabels.AttributesConfig
type ResourceAttributesConfig = otlplabels.ResourceAttributesConfig

const (
	IndexLabel         = otlplabels.IndexLabel
	StructuredMetadata = otlplabels.StructuredMetadata
	Drop               = otlplabels.Drop
)

func DefaultOTLPConfig(cfg GlobalOTLPConfig) OTLPConfig {
	return otlplabels.DefaultOTLPConfig(cfg)
}

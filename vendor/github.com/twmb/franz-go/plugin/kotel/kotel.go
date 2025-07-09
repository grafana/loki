package kotel

import (
	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	instrumentationName = "github.com/twmb/franz-go/plugin/kotel"
)

// Kotel represents the configuration options available for the kotel plugin.
type Kotel struct {
	meter  *Meter
	tracer *Tracer
}

// Opt interface used for setting optional kotel properties.
type Opt interface{ apply(*Kotel) }

type optFunc func(*Kotel)

func (o optFunc) apply(c *Kotel) { o(c) }

// WithTracer configures Kotel with a Tracer.
func WithTracer(t *Tracer) Opt {
	return optFunc(func(k *Kotel) {
		if t != nil {
			k.tracer = t
		}
	})
}

// WithMeter configures Kotel with a Meter.
func WithMeter(m *Meter) Opt {
	return optFunc(func(k *Kotel) {
		if m != nil {
			k.meter = m
		}
	})
}

// Hooks return a list of kgo.hooks compatible with its interface.
func (k *Kotel) Hooks() []kgo.Hook {
	var hooks []kgo.Hook
	if k.tracer != nil {
		hooks = append(hooks, k.tracer)
	}
	if k.meter != nil {
		hooks = append(hooks, k.meter)
	}
	return hooks
}

// NewKotel creates a new Kotel struct and applies opts to it.
func NewKotel(opts ...Opt) *Kotel {
	k := &Kotel{}
	for _, opt := range opts {
		opt.apply(k)
	}
	return k
}

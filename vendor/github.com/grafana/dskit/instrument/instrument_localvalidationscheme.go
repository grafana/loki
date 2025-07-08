//go:build localvalidationscheme

package instrument

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

func observeWithExemplar(observer prometheus.ExemplarObserver, value float64, exemplar prometheus.Labels, scheme model.ValidationScheme) {
	observer.ObserveWithExemplar(value, exemplar, scheme)
}

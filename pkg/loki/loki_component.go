package loki

import "github.com/grafana/loki/pkg/distributor"

func (t *Loki) GetDistributor() *distributor.Distributor {
	return t.distributor
}

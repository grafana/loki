package main

import "github.com/grafana/loki/v3/pkg/engine/planner/physical"

type catalog struct {
	streamsByObject map[string][]int64
}

// ResolveDataObj implements Catalog.
func (t *catalog) ResolveDataObj(physical.Expression) ([]physical.DataObjLocation, [][]int64, error) {
	objects := make([]physical.DataObjLocation, 0, len(t.streamsByObject))
	streams := make([][]int64, 0, len(t.streamsByObject))
	for o, s := range t.streamsByObject {
		objects = append(objects, physical.DataObjLocation(o))
		streams = append(streams, s)
	}
	return objects, streams, nil
}

var _ physical.Catalog = (*catalog)(nil)

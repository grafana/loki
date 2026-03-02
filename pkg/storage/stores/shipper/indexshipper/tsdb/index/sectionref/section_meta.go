package sectionref

import "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"

type SectionMeta struct {
	SectionRef
	index.ChunkMeta
}

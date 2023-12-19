package bloomcompactor

import "github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper"

func matchingBlocks(metas []bloomshipper.Meta, job Job) ([]bloomshipper.Meta, []bloomshipper.BlockRef) {
	var metasMatchingJob []bloomshipper.Meta
	var blocksMatchingJob []bloomshipper.BlockRef
	oldTombstonedBlockRefs := make(map[bloomshipper.BlockRef]struct{})

	for _, meta := range metas {
		if meta.TableName != job.tableName {
			continue
		}
		metasMatchingJob = append(metasMatchingJob, meta)

		for _, tombstonedBlockRef := range meta.Tombstones {
			oldTombstonedBlockRefs[tombstonedBlockRef] = struct{}{}
		}
	}

	for _, meta := range metasMatchingJob {
		for _, blockRef := range meta.Blocks {
			if _, ok := oldTombstonedBlockRefs[blockRef]; ok {
				// skip any previously tombstoned blockRefs
				continue
			}

			if blockRef.IndexPath == job.indexPath {
				// index has not changed, no compaction needed
				continue
			}
			blocksMatchingJob = append(blocksMatchingJob, blockRef)
		}
	}

	return metasMatchingJob, blocksMatchingJob
}

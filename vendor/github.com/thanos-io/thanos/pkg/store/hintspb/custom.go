// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package hintspb

import "github.com/oklog/ulid"

func (m *SeriesResponseHints) AddQueriedBlock(id ulid.ULID) {
	m.QueriedBlocks = append(m.QueriedBlocks, Block{
		Id: id.String(),
	})
}

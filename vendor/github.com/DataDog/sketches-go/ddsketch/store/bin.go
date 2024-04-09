// Unless explicitly stated otherwise all files in this repository are licensed
// under the Apache License 2.0.
// This product includes software developed at Datadog (https://www.datadoghq.com/).
// Copyright 2021 Datadog, Inc.

package store

import "errors"

type Bin struct {
	index int
	count float64
}

func NewBin(index int, count float64) (*Bin, error) {
	if count < 0 {
		return nil, errors.New("The count cannot be negative")
	}
	return &Bin{index: index, count: count}, nil
}

func (b Bin) Index() int {
	return b.index
}

func (b Bin) Count() float64 {
	return b.count
}

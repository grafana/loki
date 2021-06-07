package persist

//	MIT License
//
//	Copyright (c) Microsoft Corporation. All rights reserved.
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in all
//	copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//	SOFTWARE

import (
	"time"
)

const (
	// StartOfStream is a constant defined to represent the start of a partition stream in EventHub.
	StartOfStream = "-1"

	// EndOfStream is a constant defined to represent the current end of a partition stream in EventHub.
	// This can be used as an offset argument in receiver creation to start receiving from the latest
	// event, instead of a specific offset or point in time.
	EndOfStream = "@latest"
)

type (
	// Checkpoint is the information needed to determine the last message processed
	Checkpoint struct {
		Offset         string    `json:"offset"`
		SequenceNumber int64     `json:"sequenceNumber"`
		EnqueueTime    time.Time `json:"enqueueTime"`
	}
)

// NewCheckpointFromStartOfStream returns a checkpoint for the start of the stream
func NewCheckpointFromStartOfStream() Checkpoint {
	return Checkpoint{
		Offset: StartOfStream,
	}
}

// NewCheckpointFromEndOfStream returns a checkpoint for the end of the stream
func NewCheckpointFromEndOfStream() Checkpoint {
	return Checkpoint{
		Offset: EndOfStream,
	}
}

// NewCheckpoint contains the information needed to checkpoint Event Hub progress
func NewCheckpoint(offset string, sequence int64, enqueueTime time.Time) Checkpoint {
	return Checkpoint{
		Offset:         offset,
		SequenceNumber: sequence,
		EnqueueTime:    enqueueTime,
	}
}

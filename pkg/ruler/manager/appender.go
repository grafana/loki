package manager

import (
	"context"
	"fmt"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/storage/remote"
)

type TestAppendable struct {
	remoteWriter remote.WriteClient
}

type TestAppender struct {
	ctx          context.Context
	remoteWriter remote.WriteClient

	labels  []labels.Labels
	samples []client.Sample
}

// from github.com/prometheus/prometheus/storage/remote/codec.go
func labelsToLabelsProto(labels labels.Labels, buf []prompb.Label) []prompb.Label {
	result := buf[:0]
	if cap(buf) < len(labels) {
		result = make([]prompb.Label, 0, len(labels))
	}
	for _, l := range labels {
		result = append(result, prompb.Label{
			Name:  l.Name,
			Value: l.Value,
		})
	}
	return result
}

func (a *TestAppender) encodeRequest(l labels.Labels, s client.Sample) ([]byte, error) {
	ts := prompb.TimeSeries{
		Labels: labelsToLabelsProto(l, nil),
		Samples: []prompb.Sample{
			prompb.Sample{
				Value:     s.Value,
				Timestamp: s.TimestampMs,
			},
		},
	}

	// Create write request
	data, err := proto.Marshal(&prompb.WriteRequest{Timeseries: []prompb.TimeSeries{ts}})
	if err != nil {
		return nil, err
	}

	// Create HTTP request
	compressed := snappy.Encode(nil, data)
	return compressed, nil
}

func (a *TestAppendable) Appender(ctx context.Context) storage.Appender {
	return &TestAppender{
		ctx:          ctx,
		remoteWriter: a.remoteWriter,
	}
}

// Add adds a sample pair for the given series. A reference number is
// returned which can be used to add further samples in the same or later
// transactions.
// Returned reference numbers are ephemeral and may be rejected in calls
// to AddFast() at any point. Adding the sample via Add() returns a new
// reference number.
// If the reference is 0 it must not be used for caching.
func (a *TestAppender) Add(l labels.Labels, t int64, v float64) (uint64, error) {
	a.labels = append(a.labels, l)

	a.samples = append(a.samples, client.Sample{
		TimestampMs: t,
		Value:       v,
	})

	fmt.Printf("GOT HERE! %v %v %v\n\n\n", l, t, v)

	return 0, nil
}

// AddFast adds a sample pair for the referenced series. It is generally
// faster than adding a sample by providing its full label set.
func (a *TestAppender) AddFast(ref uint64, t int64, v float64) error {
	return storage.ErrNotFound
}

// Commit submits the collected samples and purges the batch. If Commit
// returns a non-nil error, it also rolls back all modifications made in
// the appender so far, as Rollback would do. In any case, an Appender
// must not be used anymore after Commit has been called.
func (a *TestAppender) Commit() error {

	if len(a.samples) == 0 {
		return nil
	}

	fmt.Printf("COMMIT TO TSDB!\n\n")
	req := client.ToWriteRequest(a.labels, a.samples, nil, client.API)
	b, err := req.Marshal()
	if err != nil {
		panic(err)
	}

	var dst []byte
	x := snappy.Encode(dst, b)

	err = a.remoteWriter.Store(a.ctx, x)
	if err != nil {
		fmt.Printf("%v", err)
	}

	return nil
}

// Rollback rolls back all modifications made in the appender so far.
// Appender has to be discarded after rollback.
func (a *TestAppender) Rollback() error {
	a.labels = nil
	a.samples = nil

	return nil
}

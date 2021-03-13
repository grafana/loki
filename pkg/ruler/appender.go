package ruler

import (
	"context"

	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/storage/remote"
)

type RemoteWriteAppendable struct {
	logger       log.Logger
	remoteWriter *remote.WriteClient
}

type RemoteWriteAppender struct {
	logger       log.Logger
	ctx          context.Context
	remoteWriter *remote.WriteClient

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

func (a *RemoteWriteAppender) encodeRequest(l labels.Labels, s client.Sample) ([]byte, error) {
	ts := prompb.TimeSeries{
		Labels: labelsToLabelsProto(l, nil),
		Samples: []prompb.Sample{
			{
				Value:     s.Value,
				Timestamp: s.TimestampMs,
			},
		},
	}

	data, err := proto.Marshal(&prompb.WriteRequest{Timeseries: []prompb.TimeSeries{ts}})
	if err != nil {
		return nil, err
	}

	compressed := snappy.Encode(nil, data)
	return compressed, nil
}

func (a *RemoteWriteAppendable) Appender(ctx context.Context) storage.Appender {
	return &RemoteWriteAppender{
		ctx:          ctx,
		logger:       a.logger,
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
func (a *RemoteWriteAppender) Add(l labels.Labels, t int64, v float64) (uint64, error) {
	a.labels = append(a.labels, l)

	a.samples = append(a.samples, client.Sample{
		TimestampMs: t,
		Value:       v,
	})

	return 0, nil
}

// AddFast adds a sample pair for the referenced series. It is generally
// faster than adding a sample by providing its full label set.
func (a *RemoteWriteAppender) AddFast(ref uint64, t int64, v float64) error {
	return storage.ErrNotFound
}

// Commit submits the collected samples and purges the batch. If Commit
// returns a non-nil error, it also rolls back all modifications made in
// the appender so far, as Rollback would do. In any case, an Appender
// must not be used anymore after Commit has been called.
func (a *RemoteWriteAppender) Commit() error {
	if a.remoteWriter == nil {
		level.Debug(a.logger).Log("msg", "no remote_write client defined, skipping commit")
		return nil
	}

	writer := *a.remoteWriter
	level.Debug(a.logger).Log("msg", "writing to remote_write target", "target", writer.Endpoint())

	req, err := a.prepareRequest()
	if err != nil {
		return err
	}

	err = writer.Store(a.ctx, req)
	if err != nil {
		return err
	}

	return nil
}

func (a *RemoteWriteAppender) prepareRequest() ([]byte, error) {
	req := client.ToWriteRequest(a.labels, a.samples, nil, client.API)
	reqBytes, err := req.Marshal()
	if err != nil {
		return nil, err
	}

	return snappy.Encode(nil, reqBytes), nil
}

// Rollback rolls back all modifications made in the appender so far.
// Appender has to be discarded after rollback.
func (a *RemoteWriteAppender) Rollback() error {
	a.labels = nil
	a.samples = nil

	return nil
}

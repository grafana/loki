package ruler

import (
	"context"
	"errors"

	"github.com/prometheus/prometheus/storage/remote"

	"github.com/prometheus/prometheus/rules"

	"github.com/prometheus/prometheus/promql"

	"github.com/prometheus/prometheus/pkg/exemplar"

	"github.com/cortexproject/cortex/pkg/cortexpb"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/storage"
)

type RemoteWriteAppendable struct {
	groupAppender map[string]*RemoteWriteAppender

	logger       log.Logger
	remoteWriter *remote.WriteClient
}

type RemoteWriteAppender struct {
	logger       log.Logger
	ctx          context.Context
	remoteWriter *remote.WriteClient
	groupKey     string

	queue *RemoteWriteQueue
}

func (a *RemoteWriteAppender) encodeRequest(l labels.Labels, s cortexpb.Sample) ([]byte, error) {
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

	if a.groupAppender == nil {
		a.groupAppender = make(map[string]*RemoteWriteAppender)
	}

	groupKey := retrieveGroupKeyFromContext(ctx)

	var appender *RemoteWriteAppender

	appender, found := a.groupAppender[groupKey]
	if !found {
		appender = &RemoteWriteAppender{
			ctx:          ctx,
			logger:       a.logger,
			remoteWriter: a.remoteWriter,
			groupKey:     groupKey,

			queue: NewRemoteWriteQueue(15000),
		}

		// only track reference if groupKey was retrieved
		if groupKey != "" {
			a.groupAppender[groupKey] = appender
		}
	}

	return appender
}

// Append adds a sample pair for the given series.
// An optional reference number can be provided to accelerate calls.
// A reference number is returned which can be used to add further
// samples in the same or later transactions.
// Returned reference numbers are ephemeral and may be rejected in calls
// to Append() at any point. Adding the sample via Append() returns a new
// reference number.
// If the reference is 0 it must not be used for caching.
func (a *RemoteWriteAppender) Append(_ uint64, l labels.Labels, t int64, v float64) (uint64, error) {
	a.queue.Append(l, cortexpb.Sample{
		Value:       v,
		TimestampMs: t,
	})

	return 0, nil
}

// AppendExemplar adds an exemplar for the given series labels.
// An optional reference number can be provided to accelerate calls.
// A reference number is returned which can be used to add further
// exemplars in the same or later transactions.
// Returned reference numbers are ephemeral and may be rejected in calls
// to Append() at any point. Adding the sample via Append() returns a new
// reference number.
// If the reference is 0 it must not be used for caching.
// Note that in our current implementation of Prometheus' exemplar storage
// calls to Append should generate the reference numbers, AppendExemplar
// generating a new reference number should be considered possible erroneous behaviour and be logged.
func (a *RemoteWriteAppender) AppendExemplar(_ uint64, _ labels.Labels, _ exemplar.Exemplar) (uint64, error) {
	return 0, errors.New("exemplars are unsupported")
}

// Commit submits the collected samples and purges the batch. If Commit
// returns a non-nil error, it also rolls back all modifications made in
// the appender so far, as Rollback would do. In any case, an Appender
// must not be used anymore after Commit has been called.
func (a *RemoteWriteAppender) Commit() error {
	if a.queue.Length() <= 0 {
		return nil
	}

	if a.remoteWriter == nil {
		level.Debug(a.logger).Log("msg", "no remote_write client defined, skipping commit")
		return nil
	}

	writer := *a.remoteWriter
	level.Warn(a.logger).Log("msg", "writing samples to remote_write target", "target", writer.Endpoint(), "count", a.queue.Length())

	req, err := a.prepareRequest()
	if err != nil {
		level.Warn(a.logger).Log("msg", "could not prepare", "err", err)
		return err
	}

	err = writer.Store(a.ctx, req)
	if err != nil {
		level.Warn(a.logger).Log("msg", "could not store", "err", err)
		return err
	}

	// clear the queue on a successful response
	a.queue.Clear()

	return nil
}

func (a *RemoteWriteAppender) prepareRequest() ([]byte, error) {
	req := cortexpb.ToWriteRequest(a.queue.labels, a.queue.samples, nil, cortexpb.RULE)
	defer cortexpb.ReuseSlice(req.Timeseries)

	reqBytes, err := req.Marshal()
	if err != nil {
		return nil, err
	}

	return snappy.Encode(nil, reqBytes), nil
}

// Rollback rolls back all modifications made in the appender so far.
// Appender has to be discarded after rollback.
func (a *RemoteWriteAppender) Rollback() error {
	a.queue.Clear()

	return nil
}

func retrieveGroupKeyFromContext(ctx context.Context) string {
	data, found := ctx.Value(promql.QueryOrigin{}).(map[string]interface{})
	if !found {
		return ""
	}

	ruleGroup, found := data["ruleGroup"].(map[string]string)
	if !found {
		return ""
	}

	file, found := ruleGroup["file"]
	if !found {
		return ""
	}

	name, found := ruleGroup["name"]
	if !found {
		return ""
	}

	return rules.GroupKey(file, name)
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

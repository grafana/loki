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
	userID        string
	cfg           Config

	logger       log.Logger
	remoteWriter *remote.WriteClient
}

type RemoteWriteAppender struct {
	logger       log.Logger
	ctx          context.Context
	remoteWriter *remote.WriteClient
	groupKey     string

	queue *remoteWriteQueue
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
	var appender *RemoteWriteAppender

	if a.groupAppender == nil {
		a.groupAppender = make(map[string]*RemoteWriteAppender)
	}

	groupKey := retrieveGroupKeyFromContext(ctx)

	appender, found := a.groupAppender[groupKey]
	if !found {
		appender = &RemoteWriteAppender{
			ctx:          ctx,
			logger:       a.logger,
			remoteWriter: a.remoteWriter,
			groupKey:     groupKey,

			queue: newRemoteWriteQueue(a.cfg.RemoteWrite.BufferSize, a.userID, groupKey),
		}

		// only track reference if groupKey was retrieved
		if groupKey == "" {
			level.Warn(a.logger).Log("msg", "blank group key passed via context; creating new appender")
			return appender
		}

		a.groupAppender[groupKey] = appender
	}

	return appender
}

func (a *RemoteWriteAppender) Append(_ uint64, l labels.Labels, t int64, v float64) (uint64, error) {
	a.queue.append(l, cortexpb.Sample{
		Value:       v,
		TimestampMs: t,
	})

	return 0, nil
}

func (a *RemoteWriteAppender) AppendExemplar(_ uint64, _ labels.Labels, _ exemplar.Exemplar) (uint64, error) {
	return 0, errors.New("exemplars are unsupported")
}

func (a *RemoteWriteAppender) Commit() error {
	if a.queue.length() <= 0 {
		return nil
	}

	if a.remoteWriter == nil {
		level.Warn(a.logger).Log("msg", "no remote_write client defined, skipping commit")
		return nil
	}

	writer := *a.remoteWriter
	level.Debug(a.logger).Log("msg", "writing samples to remote_write target", "target", writer.Endpoint(), "count", a.queue.length())

	req, err := a.prepareRequest()
	if err != nil {
		level.Error(a.logger).Log("msg", "could not prepare remote-write request", "err", err)
		return err
	}

	err = writer.Store(a.ctx, req)
	if err != nil {
		level.Error(a.logger).Log("msg", "could not store recording rule samples", "err", err)
		return err
	}

	// clear the queue on a successful response
	a.queue.clear()

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

func (a *RemoteWriteAppender) Rollback() error {
	a.queue.clear()

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

package cloudflare

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/cloudflare-go"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/jsonparser"
	"github.com/prometheus/common/model"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/positions"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// The minimun window size is 1 minute.
const minDelay = time.Minute

var cloudflareTooEarlyError = regexp.MustCompile(`too early: logs older than \S+ are not available`)

var defaultBackoff = backoff.Config{
	MinBackoff: 1 * time.Second,
	MaxBackoff: 10 * time.Second,
	MaxRetries: 5,
}

type Target struct {
	logger    log.Logger
	handler   api.EntryHandler
	positions positions.Positions
	config    *scrapeconfig.CloudflareConfig
	metrics   *Metrics

	client  Client
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	to      time.Time // the end of the next pull interval
	running *atomic.Bool
	err     error
}

func NewTarget(
	metrics *Metrics,
	logger log.Logger,
	handler api.EntryHandler,
	position positions.Positions,
	config *scrapeconfig.CloudflareConfig,
) (*Target, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	fields, err := Fields(FieldsType(config.FieldsType), config.AdditionalFields)
	if err != nil {
		return nil, err
	}
	client, err := getClient(config.APIToken, config.ZoneID, fields)
	if err != nil {
		return nil, err
	}
	pos, err := position.Get(positions.CursorKey(config.ZoneID))
	if err != nil {
		return nil, err
	}
	to := time.Now()
	if pos != 0 {
		to = time.Unix(0, pos)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t := &Target{
		logger:    logger,
		handler:   handler,
		positions: position,
		config:    config,
		metrics:   metrics,

		ctx:     ctx,
		cancel:  cancel,
		client:  client,
		to:      to,
		running: atomic.NewBool(false),
	}
	t.start()
	return t, nil
}

func (t *Target) start() {
	t.wg.Add(1)
	t.running.Store(true)
	go func() {
		defer func() {
			t.wg.Done()
			t.running.Store(false)
		}()
		for t.ctx.Err() == nil {
			end := t.to
			maxEnd := time.Now().Add(-minDelay)
			if end.After(maxEnd) {
				end = maxEnd
			}
			start := end.Add(-time.Duration(t.config.PullRange))
			requests := splitRequests(start, end, t.config.Workers)
			// Use background context for workers as we don't want to cancel half way through.
			// In case of errors we stop the target, each worker has it's own retry logic.
			if err := concurrency.ForEachJob(context.Background(), len(requests), t.config.Workers, func(ctx context.Context, idx int) error {
				request := requests[idx]
				return t.pull(ctx, request.start, request.end)
			}); err != nil {
				level.Error(t.logger).Log("msg", "failed to pull logs", "err", err, "start", start, "end", end)
				t.err = err
				return
			}

			// Sets current timestamp metrics, move to the next interval and saves the position.
			t.metrics.LastEnd.Set(float64(end.UnixNano()) / 1e9)
			t.to = end.Add(time.Duration(t.config.PullRange))
			t.positions.Put(positions.CursorKey(t.config.ZoneID), t.to.UnixNano())

			// If the next window can be fetched do it, if not sleep for a while.
			// This is because Cloudflare logs should never be pulled between now-1m and now.
			diff := t.to.Sub(time.Now().Add(-minDelay))
			if diff > 0 {
				select {
				case <-time.After(diff):
				case <-t.ctx.Done():
				}
			}
		}
	}()
}

// pull pulls logs from cloudflare for a given time range.
// It will retry on errors.
func (t *Target) pull(ctx context.Context, start, end time.Time) error {
	var (
		backoff = backoff.New(ctx, defaultBackoff)
		errs    = multierror.New()
		it      cloudflare.LogpullReceivedIterator
		err     error
	)

	for backoff.Ongoing() {
		it, err = t.client.LogpullReceived(ctx, start, end)
		if err != nil && cloudflareTooEarlyError.MatchString(err.Error()) {
			level.Warn(t.logger).Log("msg", "failed iterating over logs, out of cloudflare range, not retrying", "err", err, "start", start, "end", end, "retries", backoff.NumRetries())
			return nil
		} else if err != nil {
			if it != nil {
				it.Close()
			}
			errs.Add(err)
			backoff.Wait()
			continue
		}
		if err := func() error {
			defer it.Close()
			var lineRead int64
			for it.Next() {
				line := it.Line()
				ts, err := jsonparser.GetInt(line, "EdgeStartTimestamp")
				if err != nil {
					ts = time.Now().UnixNano()
				}
				t.handler.Chan() <- api.Entry{
					Labels: t.config.Labels.Clone(),
					Entry: logproto.Entry{
						Timestamp: time.Unix(0, ts),
						Line:      string(line),
					},
				}
				lineRead++
				t.metrics.Entries.Inc()
			}
			if it.Err() != nil {
				level.Warn(t.logger).Log("msg", "failed iterating over logs", "err", it.Err(), "start", start, "end", end, "retries", backoff.NumRetries(), "lineRead", lineRead)
				return it.Err()
			}
			return nil
		}(); err != nil {
			errs.Add(err)
			backoff.Wait()
			continue
		}
		return nil

	}
	return errs.Err()
}

func (t *Target) Stop() {
	t.cancel()
	t.wg.Wait()
	t.handler.Stop()
}

func (t *Target) Type() target.TargetType {
	return target.CloudflareTargetType
}

func (t *Target) DiscoveredLabels() model.LabelSet {
	return nil
}

func (t *Target) Labels() model.LabelSet {
	return t.config.Labels
}

func (t *Target) Ready() bool {
	return t.running.Load()
}

func (t *Target) Details() interface{} {
	fields, _ := Fields(FieldsType(t.config.FieldsType), t.config.AdditionalFields)
	var errMsg string
	if t.err != nil {
		errMsg = t.err.Error()
	}
	return map[string]string{
		"zone_id":        t.config.ZoneID,
		"error":          errMsg,
		"position":       t.positions.GetString(positions.CursorKey(t.config.ZoneID)),
		"last_timestamp": t.to.String(),
		"fields":         strings.Join(fields, ","),
	}
}

type pullRequest struct {
	start time.Time
	end   time.Time
}

func splitRequests(start, end time.Time, workers int) []pullRequest {
	perWorker := end.Sub(start) / time.Duration(workers)
	var requests []pullRequest
	for i := 0; i < workers; i++ {
		r := pullRequest{
			start: start.Add(time.Duration(i) * perWorker),
			end:   start.Add(time.Duration(i+1) * perWorker),
		}
		// If the last worker is smaller than the others, we need to make sure it gets the last chunk.
		if i == workers-1 && r.end != end {
			r.end = end
		}
		requests = append(requests, r)
	}
	return requests
}

func validateConfig(cfg *scrapeconfig.CloudflareConfig) error {
	if cfg.FieldsType == "" {
		cfg.FieldsType = string(FieldsTypeDefault)
	}
	if cfg.APIToken == "" {
		return errors.New("cloudflare api token is required")
	}
	if cfg.ZoneID == "" {
		return errors.New("cloudflare zone id is required")
	}
	if cfg.PullRange == 0 {
		cfg.PullRange = model.Duration(time.Minute)
	}
	if cfg.Workers == 0 {
		cfg.Workers = 3
	}
	return nil
}

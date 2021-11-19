package cloudflare

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/buger/jsonparser"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/concurrency"
	"github.com/grafana/loki/clients/pkg/promtail/api"
	"github.com/grafana/loki/clients/pkg/promtail/positions"
	"github.com/grafana/loki/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/clients/pkg/promtail/targets/target"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/common/model"
	"go.uber.org/atomic"
)

type Target struct {
	logger    log.Logger
	handler   api.EntryHandler
	positions positions.Positions
	config    *scrapeconfig.CloudflareConfig

	client  Client
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
	to      time.Time // the end of the next pull interval
	running *atomic.Bool
}

func NewTarget(
	logger log.Logger,
	handler api.EntryHandler,
	posi positions.Positions,
	config *scrapeconfig.CloudflareConfig,
) (*Target, error) {
	if err := validateConfig(config); err != nil {
		return nil, err
	}
	client, err := getClient(config.APIToken, config.ZoneID, nil)
	if err != nil {
		return nil, err
	}
	pos, err := posi.Get(positions.CursorKey(config.ZoneID))
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
		positions: posi,
		config:    config,

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
		for {
			if t.ctx.Err() != nil {
				return
			}
			end := t.to
			maxEnd := time.Now().Add(-time.Minute)
			if end.After(maxEnd) {
				end = maxEnd
			}
			start := end.Add(-t.config.PullRange)

			err := concurrency.ForEach(context.Background(), splitRequests(start, end, t.config.Workers), t.config.Workers, func(ctx context.Context, job interface{}) error {
				request := job.(pullRequest)
				return t.pull(ctx, request.start, request.end)
			})
			if err != nil {
				// todo handle errors.
				// log.Println(err)
			}
			// move to the next interval and save the position
			t.to = end.Add(t.config.PullRange)
			t.positions.Put(positions.CursorKey(t.config.ZoneID), t.to.UnixNano())

			// if the next window can be fetch do it, if not sleep for a while
			diff := t.to.Sub(time.Now().Add(-time.Minute))
			if diff > 0 {
				select {
				case <-time.After(diff):
				case <-t.ctx.Done():
				}
			}
			// todo metrics about the lag.
		}
	}()
}

func (t *Target) pull(ctx context.Context, start, end time.Time) error {
	it, err := t.client.LogpullReceived(ctx, start, end)
	if err != nil {
		return err
	}
	defer it.Close()
	for it.Next() {
		if it.Err() != nil {
			return it.Err()
		}
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
	}
	return nil
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
	return nil
}

type pullRequest struct {
	start time.Time
	end   time.Time
}

func splitRequests(start, end time.Time, workers int) []interface{} {
	perWorker := end.Sub(start) / time.Duration(workers)
	var requests []interface{}
	for i := 0; i < workers; i++ {
		r := pullRequest{
			start: start.Add(time.Duration(i) * perWorker),
			end:   start.Add(time.Duration(i+1) * perWorker),
		}
		if r.end.After(end) {
			r.end = end
		}
		requests = append(requests, r)
	}
	return requests
}

func validateConfig(cfg *scrapeconfig.CloudflareConfig) error {
	if cfg.APIToken == "" {
		return errors.New("cloudflare api token is required")
	}
	if cfg.ZoneID == "" {
		return errors.New("cloudflare zone id is required")
	}
	if cfg.PullRange == 0 {
		cfg.PullRange = time.Minute
	}
	if cfg.Workers == 0 {
		cfg.Workers = 3
	}
	return nil
}

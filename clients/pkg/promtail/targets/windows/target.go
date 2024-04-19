//go:build windows
// +build windows

package windows

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/spf13/afero"
	"golang.org/x/sys/windows"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
	"github.com/grafana/loki/v3/clients/pkg/promtail/scrapeconfig"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/target"
	"github.com/grafana/loki/v3/clients/pkg/promtail/targets/windows/win_eventlog"
)

var fs = afero.NewOsFs()

type Target struct {
	subscription  win_eventlog.EvtHandle
	handler       api.EntryHandler
	cfg           *scrapeconfig.WindowsEventsTargetConfig
	relabelConfig []*relabel.Config
	logger        log.Logger

	bm      *bookMark // bookmark to save positions.
	fetcher *win_eventlog.EventFetcher

	ready bool
	done  chan struct{}
	wg    sync.WaitGroup
	err   error
}

// New create a new windows targets, that will fetch windows event logs and send them to Loki.
func New(
	logger log.Logger,
	handler api.EntryHandler,
	relabel []*relabel.Config,
	cfg *scrapeconfig.WindowsEventsTargetConfig,
) (*Target, error) {
	sigEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(sigEvent)

	bm, err := newBookMark(cfg.BookmarkPath)
	if err != nil {
		return nil, fmt.Errorf("failed to create bookmark using path=%s: %w", cfg.BookmarkPath, err)
	}

	t := &Target{
		done:          make(chan struct{}),
		cfg:           cfg,
		bm:            bm,
		relabelConfig: relabel,
		logger:        logger,
		handler:       handler,
		fetcher:       win_eventlog.NewEventFetcher(),
	}

	if cfg.Query == "" {
		cfg.Query = "*"
	}

	var subsHandle win_eventlog.EvtHandle
	if bm.isNew {
		subsHandle, err = win_eventlog.EvtSubscribe(cfg.EventlogName, cfg.Query)
	} else {
		subsHandle, err = win_eventlog.EvtSubscribeWithBookmark(cfg.EventlogName, cfg.Query, bm.handle)
	}

	if err != nil {
		return nil, fmt.Errorf("error subscribing to windows events: %w", err)
	}
	t.subscription = subsHandle

	if t.cfg.PollInterval == 0 {
		t.cfg.PollInterval = 3 * time.Second
	}
	go t.loop()
	return t, nil
}

// loop fetches new events and send them to via the Loki client.
func (t *Target) loop() {
	t.ready = true
	t.wg.Add(1)
	interval := time.NewTicker(t.cfg.PollInterval)
	defer func() {
		t.ready = false
		t.wg.Done()
		interval.Stop()
	}()

	for {

	loop:
		for {
			// fetch events until there's no more.
			events, handles, err := t.fetcher.FetchEvents(t.subscription, t.cfg.Locale)
			if err != nil {
				if err != win_eventlog.ERROR_NO_MORE_ITEMS {
					t.err = err
					level.Error(t.logger).Log("msg", "error fetching events", "err", err)
				}
				break loop
			}
			t.err = nil
			// we have received events to handle.
			for i, entry := range t.renderEntries(events) {
				t.handler.Chan() <- entry
				if err := t.bm.save(handles[i]); err != nil {
					t.err = err
					level.Error(t.logger).Log("msg", "error saving bookmark", "err", err)
				}
			}
			win_eventlog.Close(handles)

		}
		// no more messages we wait for next poll timer tick.
		select {
		case <-t.done:
			return
		case <-interval.C:
		}
	}
}

// renderEntries renders Loki entries from windows event logs
func (t *Target) renderEntries(events []win_eventlog.Event) []api.Entry {
	res := make([]api.Entry, 0, len(events))
	lbs := labels.NewBuilder(nil)
	for _, event := range events {
		entry := api.Entry{
			Labels: make(model.LabelSet),
		}

		entry.Timestamp = time.Now()
		if t.cfg.UseIncomingTimestamp {
			timeStamp, err := time.Parse(time.RFC3339Nano, fmt.Sprintf("%v", event.TimeCreated.SystemTime))
			if err != nil {
				level.Warn(t.logger).Log("msg", "error parsing timestamp", "err", err)
			} else {
				entry.Timestamp = timeStamp
			}
		}
		// Add constant labels
		for k, v := range t.cfg.Labels {
			lbs.Set(string(k), string(v))
		}
		// discover labels
		if channel := model.LabelValue(event.Channel); channel != "" && channel.IsValid() {
			lbs.Set("channel", event.Channel)
		}
		if computer := model.LabelValue(event.Computer); computer != "" && computer.IsValid() {
			lbs.Set("computer", event.Computer)
		}
		// apply relabelings.
		processed, _ := relabel.Process(lbs.Labels(), t.relabelConfig...)

		for _, lbl := range processed {
			if strings.HasPrefix(lbl.Name, "__") {
				continue
			}
			entry.Labels[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
		}

		line, err := formatLine(t.cfg, event)
		if err != nil {
			level.Warn(t.logger).Log("msg", "error formatting event", "err", err)
			continue
		}
		entry.Line = line
		res = append(res, entry)
	}
	return res
}

// Type returns WindowsTargetType.
func (t *Target) Type() target.TargetType {
	return target.WindowsTargetType
}

// Ready indicates whether or not the windows target is ready.
func (t *Target) Ready() bool {
	if t.err != nil {
		return false
	}
	return t.ready
}

// DiscoveredLabels returns discovered labels from the target.
func (t *Target) DiscoveredLabels() model.LabelSet {
	// todo(cyriltovena) we might want to sample discovered labels later and returns them here.
	return nil
}

// Labels returns the set of labels that statically apply to all log entries
// produced by the windows target.
func (t *Target) Labels() model.LabelSet {
	return t.cfg.Labels
}

// Details returns target-specific details.
func (t *Target) Details() interface{} {
	if t.err != nil {
		return map[string]string{"err": t.err.Error()}
	}
	return map[string]string{}
}

func (t *Target) Stop() error {
	close(t.done)
	t.wg.Wait()
	t.handler.Stop()
	if err := t.bm.close(); err != nil {
		return err
	}
	return t.err
}

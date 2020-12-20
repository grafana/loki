//+build windows
package windows

import (
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/spf13/afero"
	"golang.org/x/sys/windows"

	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/grafana/loki/pkg/promtail/targets/target"
	"github.com/grafana/loki/pkg/promtail/targets/windows/win_eventlog"
	"github.com/prometheus/prometheus/pkg/labels"
)

var (
	fs = afero.NewOsFs()
)

type Target struct {
	subscription  win_eventlog.EvtHandle
	handler       api.EntryHandler
	cfg           Config
	relabelConfig []*relabel.Config
	logger        log.Logger

	bm *bookMark // bookmark to save positions.

	ready bool
	done  chan struct{}
	wg    sync.WaitGroup
	err   error
}

type Config struct {
	win_eventlog.WinEventLog `yaml:",inline"`
	BoorkmarkPath            string        `yaml:"bookmark_path"`
	PollInterval             time.Duration `yaml:"poll_interval"`
	// Labels optionally holds labels to associate with each record received on the push api.
	Labels model.LabelSet `yaml:"labels"`
}

func New(
	logger log.Logger,
	handler api.EntryHandler,
	relabel []*relabel.Config,
	cfg Config,
) (*Target, error) {
	sigEvent, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(sigEvent)

	bm, err := newBookMark(cfg.BoorkmarkPath)
	if err != nil {
		return nil, err
	}

	t := &Target{
		done:          make(chan struct{}),
		cfg:           cfg,
		bm:            bm,
		relabelConfig: relabel,
		logger:        logger,
		handler:       handler,
	}

	var subsHandle win_eventlog.EvtHandle
	if bm.isNew {
		subsHandle, err = win_eventlog.EvtSubscribe(cfg.EventlogName, cfg.Query)
	} else {
		subsHandle, err = win_eventlog.EvtSubscribeWithBookmark(cfg.EventlogName, cfg.Query, bm.handle)
	}

	if err != nil {
		return nil, fmt.Errorf("evtSubscribe: %w", err)
	}
	t.subscription = subsHandle
	// todo move to default conf
	if t.cfg.PollInterval == 0 {
		t.cfg.PollInterval = 3 * time.Second
	}
	go t.loop()
	return t, nil
}

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
			events, err := t.cfg.FetchEvents(t.subscription)
			if err != nil {
				if err != win_eventlog.ERROR_NO_MORE_ITEMS {
					t.err = err
					level.Error(util.Logger).Log("msg", "error fetching events", "err", err)
				}
				break loop
			}
			t.err = nil
			// we have received events to handle.
			for _, entry := range t.renderEntries(events) {
				t.handler.Chan() <- entry
			}

		}
		// no more messages we wait for next poll timer tick.
		select {
		case <-t.done:
			return
		case <-interval.C:
		}
	}
}

func (t *Target) renderEntries(events []win_eventlog.Event) []api.Entry {
	res := make([]api.Entry, 0, len(events))
	lbs := labels.NewBuilder(nil)
	for _, event := range events {
		entry := api.Entry{
			Labels: make(model.LabelSet),
		}

		if t.cfg.TimeStampFromEvent {
			timeStamp, err := time.Parse(time.RFC3339Nano, fmt.Sprintf("%v", event.TimeCreated.SystemTime))
			if err != nil {
				level.Warn(t.logger).Log("msg", "error parsing timestamp", "err", err)
				continue
			}
			entry.Timestamp = timeStamp
		}
		lbs.Reset(nil)
		// Add constant labels
		for k, v := range t.cfg.Labels {
			lbs.Set(string(k), string(v))
		}
		// discover labels
		// todo
		// apply relabelings.
		processed := relabel.Process(lbs.Labels(), t.relabelConfig...)

		for _, lbl := range processed {
			if strings.HasPrefix(lbl.Name, "__") {
				continue
			}
			entry.Labels[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
		}
		// format line message.
		// relabel
		res = append(res, entry)
	}
	return res
}

// Type returns SyslogTargetType.
func (t *Target) Type() target.TargetType {
	return target.WindowsTargetType
}

// Ready indicates whether or not the syslog target is ready to be read from.
func (t *Target) Ready() bool {
	if t.err != nil {
		return false
	}
	return true
}

func (t *Target) DiscoveredLabels() model.LabelSet {
	return nil
}

// Labels returns the set of labels that statically apply to all log entries
// produced by the SyslogTarget.
func (t *Target) Labels() model.LabelSet {
	return nil
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
	return t.err
}

type bookMark struct {
	handle win_eventlog.EvtHandle
	file   afero.File
	isNew  bool
}

func newBookMark(path string) (*bookMark, error) {
	_, err := fs.Stat(path)
	if os.IsNotExist(err) {
		file, err := fs.Create(path)
		if err != nil {
			return nil, err
		}
		bm, err := win_eventlog.CreateBookmark("")
		if err != nil {
			return nil, err
		}
		return &bookMark{handle: bm, file: file, isNew: true}, nil
	}
	if err != nil {
		return nil, err
	}
	file, err := fs.Open(path)
	if err != nil {
		return nil, err
	}
	fileContent, err := ioutil.ReadAll(file)
	if err != nil {
		return nil, err
	}
	fileString := string(fileContent)
	bm, err := win_eventlog.CreateBookmark(fileString)
	if err != nil {
		return nil, err
	}
	return &bookMark{handle: bm, file: file, isNew: fileString == ""}, nil
}

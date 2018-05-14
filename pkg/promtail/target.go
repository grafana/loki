package promtail

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"time"

	"github.com/hpcloud/tail"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	fsnotify "gopkg.in/fsnotify.v1"
)

type Target struct {
	labels    model.LabelSet
	client    *Client
	positions *Positions

	watcher *fsnotify.Watcher
	path    string
	quit    chan struct{}

	tails map[string]*tailer
}

func NewTarget(c *Client, positions *Positions, path string, labels model.LabelSet) (*Target, error) {
	log.Info("newTarget", labels)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := watcher.Add(path); err != nil {
		watcher.Close()
		return nil, err
	}

	t := &Target{
		labels:    labels,
		watcher:   watcher,
		path:      path,
		client:    c,
		positions: positions,
		quit:      make(chan struct{}),
		tails:     map[string]*tailer{},
	}

	// Fist, we're going to add all the existing files
	fis, err := ioutil.ReadDir(t.path)
	if err != nil {
		return nil, err
	}
	for _, fi := range fis {
		if fi.IsDir() {
			continue
		}

		tailer, err := newTailer(t, t.path, fi.Name())
		if err != nil {
			log.Errorf("Failed to tail file: %v", err)
			continue
		}

		t.tails[fi.Name()] = tailer
	}

	go t.run()
	return t, nil
}

func (t *Target) Stop() {
	close(t.quit)
}

func (t *Target) run() {
	defer func() {
		t.watcher.Close()
		for _, v := range t.tails {
			v.stop()
		}
	}()

	for {
		select {
		case event := <-t.watcher.Events:
			switch event.Op {
			case fsnotify.Create:
				// protect against double Creates.
				if _, ok := t.tails[event.Name]; ok {
					log.Infof("Got 'create' for existing file '%s'", event.Name)
					continue
				}

				tailer, err := newTailer(t, t.path, event.Name)
				if err != nil {
					log.Errorf("Failed to tail file: %v", err)
					continue
				}

				t.tails[event.Name] = tailer

			case fsnotify.Remove:
				tailer, ok := t.tails[event.Name]
				if ok {
					tailer.stop()
					delete(t.tails, event.Name)
				}

			default:
				log.Println("event:", event)
			}
		case err := <-t.watcher.Errors:
			log.Println("error:", err)
		case <-t.quit:
			return
		}
	}
}

func (t *Target) handleLine(_ time.Time, line string) error {
	// Hard code to deal with docker-style json for now; eventually we'll use
	// relabelling.
	var entry struct {
		Log    string
		Stream string
		Time   time.Time
	}
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		return err
	}
	return t.client.Line(t.labels, entry.Time, entry.Log)
}

type tailer struct {
	target *Target
	path   string
	tail   *tail.Tail
	quit   chan struct{}
}

func newTailer(target *Target, dir, name string) (*tailer, error) {
	path := filepath.Join(dir, name)
	tail, err := tail.TailFile(path, tail.Config{
		Follow: true,
		Location: &tail.SeekInfo{
			Offset: target.positions.Get(path),
			Whence: 0,
		},
	})
	if err != nil {
		return nil, err
	}

	tailer := &tailer{
		target: target,
		path:   path,
		tail:   tail,
		quit:   make(chan struct{}),
	}
	go tailer.run()
	return tailer, nil
}

func (t *tailer) run() {
	defer func() {
		t.tail.Stop()
		log.Infof("Stopping tailing file: %s", t.path)
	}()

	log.Infof("Tailing file: %s", t.path)
	positionSyncPeriod := t.target.positions.cfg.SyncPeriod
	positionWait := time.NewTimer(positionSyncPeriod)
	for {
		select {
		case <-t.quit:
			return

		case <-positionWait.C:
			pos, err := t.tail.Tell()
			if err != nil {
				log.Errorf("Error getting tail position: %v", err)
				continue
			}
			t.target.positions.Put(t.path, pos)

		case line := <-t.tail.Lines:
			if line.Err != nil {
				log.Errorf("error reading line: %v", line.Err)
			}

			if err := t.target.handleLine(line.Time, line.Text); err != nil {
				log.Infof("error handling line: %v", err)
			}
		}
	}
}

func (t *tailer) stop() {
	close(t.quit)
}

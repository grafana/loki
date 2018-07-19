package file

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
	"time"

	"github.com/grafana/logish/pkg/promtail"
	"github.com/hpcloud/tail"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	log "github.com/sirupsen/logrus"
	fsnotify "gopkg.in/fsnotify.v1"
)

var (
	readBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "promtail",
		Name:      "read_bytes_total",
		Help:      "Number of bytes read.",
	}, []string{"path"})
)

func init() {
	prometheus.MustRegister(readBytes)
}

type FileTarget struct {
	labels    model.LabelSet
	client    *promtail.Client
	positions *promtail.Positions

	watcher *fsnotify.Watcher
	path    string
	quit    chan struct{}

	tails map[string]*tailer
}

func NewFileTarget(c *promtail.Client, positions *promtail.Positions, path string, labels model.LabelSet) (*FileTarget, error) {
	log.Info("new file target", labels)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	if err := watcher.Add(path); err != nil {
		watcher.Close()
		return nil, err
	}

	t := &FileTarget{
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

func (t *FileTarget) Stop() error {
	close(t.quit)

	return nil
}

func (t *FileTarget) run() {
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

func (t *FileTarget) handleLine(_ time.Time, line string) error {
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
	target *FileTarget
	path   string
	tail   *tail.Tail
}

func newTailer(target *FileTarget, dir, name string) (*tailer, error) {
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
	}
	go tailer.run()
	return tailer, nil
}

func (t *tailer) run() {
	defer func() {
		log.Infof("Stopping tailing file: %s", t.path)
	}()

	log.Infof("Tailing file: %s", t.path)
	positionSyncPeriod := t.target.positions.SyncPeriod()
	positionWait := time.NewTimer(positionSyncPeriod)
	defer positionWait.Stop()

	for {
		select {

		case <-positionWait.C:
			pos, err := t.tail.Tell()
			if err != nil {
				log.Errorf("Error getting tail position: %v", err)
				continue
			}
			t.target.positions.Put(t.path, pos)

		case line, ok := <-t.tail.Lines:
			if !ok {
				return
			}

			if line.Err != nil {
				log.Errorf("error reading line: %v", line.Err)
			}

			readBytes.WithLabelValues(t.path).Add(float64(len(line.Text)))
			if err := t.target.handleLine(line.Time, line.Text); err != nil {
				log.Infof("error handling line: %v", err)
			}
		}
	}
}

func (t *tailer) stop() {
	t.tail.Stop()
}

package positions

import (
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	yaml "gopkg.in/yaml.v2"
)

const positionFileMode = 0600

// Config describes where to get postition information from.
type Config struct {
	SyncPeriod    time.Duration `yaml:"sync_period"`
	PositionsFile string        `yaml:"filename"`
}

// RegisterFlags register flags.
func (cfg *Config) RegisterFlags(flags *flag.FlagSet) {
	flags.DurationVar(&cfg.SyncPeriod, "positions.sync-period", 10*time.Second, "Period with this to sync the position file.")
	flag.StringVar(&cfg.PositionsFile, "positions.file", "/var/log/positions.yaml", "Location to read/write positions from.")
}

// Positions tracks how far through each file we've read.
type Positions struct {
	logger    log.Logger
	cfg       Config
	mtx       sync.Mutex
	positions map[string]string
	quit      chan struct{}
	done      chan struct{}
}

// File format for the positions data.
type File struct {
	Positions map[string]string `yaml:"positions"`
}

// New makes a new Positions.
func New(logger log.Logger, cfg Config) (*Positions, error) {
	positions, err := readPositionsFile(cfg.PositionsFile)
	if err != nil {
		return nil, err
	}

	p := &Positions{
		logger:    logger,
		cfg:       cfg,
		positions: positions,
		quit:      make(chan struct{}),
		done:      make(chan struct{}),
	}

	go p.run()
	return p, nil
}

// Stop the Position tracker.
func (p *Positions) Stop() {
	close(p.quit)
	<-p.done
}

// PutString records (asynchronsouly) how far we've read through a file.
// Unlike Put, it records a string offset and is only useful for
// JournalTargets which doesn't have integer offsets.
func (p *Positions) PutString(path string, pos string) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.positions[path] = pos
}

// Put records (asynchronously) how far we've read through a file.
func (p *Positions) Put(path string, pos int64) {
	p.PutString(path, strconv.FormatInt(pos, 10))
}

// GetString returns how far we've through a file as a string.
// JournalTarget writes a journal cursor to the positions file, while
// FileTarget writes an integer offset. Use Get to read the integer
// offset.
func (p *Positions) GetString(path string) string {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	return p.positions[path]
}

// Get returns how far we've read through a file. Returns an error
// if the value stored for the file is not an integer.
func (p *Positions) Get(path string) (int64, error) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	pos, ok := p.positions[path]
	if !ok {
		return 0, nil
	}
	return strconv.ParseInt(pos, 10, 64)
}

// Remove removes the position tracking for a filepath
func (p *Positions) Remove(path string) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.remove(path)
}

func (p *Positions) remove(path string) {
	delete(p.positions, path)
}

// SyncPeriod returns how often the positions file gets resynced
func (p *Positions) SyncPeriod() time.Duration {
	return p.cfg.SyncPeriod
}

func (p *Positions) run() {
	defer func() {
		p.save()
		level.Debug(p.logger).Log("msg", "positions saved")
		close(p.done)
	}()

	ticker := time.NewTicker(p.cfg.SyncPeriod)
	for {
		select {
		case <-p.quit:
			return
		case <-ticker.C:
			p.save()
			p.cleanup()
		}
	}
}

func (p *Positions) save() {
	p.mtx.Lock()
	positions := make(map[string]string, len(p.positions))
	for k, v := range p.positions {
		positions[k] = v
	}
	p.mtx.Unlock()

	if err := writePositionFile(p.cfg.PositionsFile, positions); err != nil {
		level.Error(p.logger).Log("msg", "error writing positions file", "error", err)
	}
}

func (p *Positions) cleanup() {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	toRemove := []string{}
	for k := range p.positions {
		// If the position file is prefixed with journal, it's a
		// JournalTarget cursor and not a file on disk.
		if strings.HasPrefix(k, "journal-") {
			continue
		}

		if _, err := os.Stat(k); err != nil {
			if os.IsNotExist(err) {
				// File no longer exists.
				toRemove = append(toRemove, k)
			} else {
				// Can't determine if file exists or not, some other error.
				level.Warn(p.logger).Log("msg", "could not determine if log file "+
					"still exists while cleaning positions file", "error", err)
			}
		}
	}
	for _, tr := range toRemove {
		p.remove(tr)
	}
}

func readPositionsFile(filename string) (map[string]string, error) {
	buf, err := ioutil.ReadFile(filepath.Clean(filename))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}

	var p File
	if err := yaml.UnmarshalStrict(buf, &p); err != nil {
		return nil, err
	}

	return p.Positions, nil
}

func writePositionFile(filename string, positions map[string]string) error {
	buf, err := yaml.Marshal(File{
		Positions: positions,
	})
	if err != nil {
		return err
	}

	return ioutil.WriteFile(filepath.Clean(filename), buf, os.FileMode(positionFileMode))
}

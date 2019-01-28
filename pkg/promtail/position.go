package promtail

import (
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"gopkg.in/yaml.v2"
)

const positionFileMode = 0700

// PositionsConfig describes where to get postition information from.
type PositionsConfig struct {
	SyncPeriod    time.Duration `yaml:"sync_period"`
	PositionsFile string        `yaml:"filename"`
}

// RegisterFlags register flags.
func (cfg *PositionsConfig) RegisterFlags(flags *flag.FlagSet) {
	flags.DurationVar(&cfg.SyncPeriod, "positions.sync-period", 10*time.Second, "Period with this to sync the position file.")
	flag.StringVar(&cfg.PositionsFile, "positions.file", "/var/log/positions.yaml", "Location to read/wrtie positions from.")
}

// Positions tracks how far through each file we've read.
type Positions struct {
	logger       log.Logger
	cfg          PositionsConfig
	mtx          sync.Mutex
	positions    map[string]int64
	quit         chan struct{}
	quitComplete chan struct{}
}

type PositionsFile struct {
	Positions map[string]int64 `yaml:"positions"`
}

// NewPositions makes a new Positions.
func NewPositions(logger log.Logger, cfg PositionsConfig) (*Positions, error) {
	positions, err := readPositionsFile(cfg.PositionsFile)
	if err != nil {
		return nil, err
	}

	p := &Positions{
		logger:       logger,
		cfg:          cfg,
		positions:    positions,
		quit:         make(chan struct{}),
		quitComplete: make(chan struct{}),
	}

	go p.run()
	return p, nil
}

// Stop the Position tracker.
func (p *Positions) Stop() {
	close(p.quit)
	<-p.quitComplete
}

// Put records (asynchronously) how far we've read through a file.
func (p *Positions) Put(path string, pos int64) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.positions[path] = pos
}

// Get returns how far we've read through a file.
func (p *Positions) Get(path string) int64 {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	return p.positions[path]
}

// Remove removes the position tracking for a filepath
func (p *Positions) Remove(path string) {
	p.mtx.Lock()
	delete(p.positions, path)
	p.mtx.Unlock()
}

func (p *Positions) run() {
	defer func() {
		p.save()
		level.Debug(p.logger).Log("msg", "positions saved")
		close(p.quitComplete)
	}()

	ticker := time.NewTicker(p.cfg.SyncPeriod)
	for {
		select {
		case <-p.quit:
			return
		case <-ticker.C:
			p.save()
		}
	}
}

func (p *Positions) save() {
	p.mtx.Lock()
	positions := make(map[string]int64, len(p.positions))
	for k, v := range p.positions {
		positions[k] = v
	}
	p.mtx.Unlock()

	if err := writePositionFile(p.cfg.PositionsFile, positions); err != nil {
		level.Error(p.logger).Log("msg", "error writing positions file", "error", err)
	}
}

func readPositionsFile(filename string) (map[string]int64, error) {
	buf, err := ioutil.ReadFile(filepath.Clean(filename))
	if err != nil {
		if os.IsNotExist(err) {
			positions := map[string]int64{}
			err = writePositionFile(filename, positions)
			return positions, err
		}
		return nil, err
	}

	var p PositionsFile
	if err := yaml.UnmarshalStrict(buf, &p); err != nil {
		return nil, err
	}

	return p.Positions, nil
}

func writePositionFile(filename string, positions map[string]int64) error {
	buf, err := yaml.Marshal(PositionsFile{
		Positions: positions,
	})
	if err != nil {
		return err
	}

	return ioutil.WriteFile(filepath.Clean(filename), buf, os.FileMode(positionFileMode))
}

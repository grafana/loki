package promtail

import (
	"flag"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"gopkg.in/yaml.v2"
)

const positionFileMode = 0700

// PositionsConfig describes where to get postition information from.
type PositionsConfig struct {
	SyncPeriod    time.Duration
	PositionsFile string
}

// RegisterFlags register flags.
func (cfg *PositionsConfig) RegisterFlags(flags *flag.FlagSet) {
	flags.DurationVar(&cfg.SyncPeriod, "positions.sync-period", 10*time.Second, "Period with this to sync the position file.")
	flag.StringVar(&cfg.PositionsFile, "positions.file", "/var/log/positions.yaml", "Location to read/wrtie positions from.")
}

// Positions tracks how far through each file we've read.
type Positions struct {
	cfg       PositionsConfig
	mtx       sync.Mutex
	positions map[string]int64
	quit      chan struct{}
}

type positionsFile struct {
	Positions map[string]int64 `yaml:"positions"`
}

// NewPositions makes a new Positions.
func NewPositions(cfg PositionsConfig) (*Positions, error) {
	positions, err := readPositionsFile(cfg.PositionsFile)
	if err != nil {
		return nil, err
	}

	p := &Positions{
		cfg:       cfg,
		positions: positions,
		quit:      make(chan struct{}),
	}

	go p.run()
	return p, nil
}

// Stop the Position tracker.
func (p *Positions) Stop() {
	close(p.quit)
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

func (p *Positions) run() {
	defer p.save()

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
		log.Errorf("Error writing positions file: %v", err)
	}
}

func readPositionsFile(filename string) (map[string]int64, error) {
	buf, err := ioutil.ReadFile(filepath.Clean(filename))
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]int64{}, nil
		}
		return nil, err
	}

	var p positionsFile
	if err := yaml.UnmarshalStrict(buf, &p); err != nil {
		return nil, err
	}

	return p.Positions, nil
}

func writePositionFile(filename string, positions map[string]int64) error {
	buf, err := yaml.Marshal(positionsFile{
		Positions: positions,
	})
	if err != nil {
		return err
	}

	return ioutil.WriteFile(filepath.Clean(filename), buf, os.FileMode(positionFileMode))
}

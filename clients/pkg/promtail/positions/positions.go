package positions

import (
	"flag"
	"fmt"
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

// Config describes where to get position information from.
type Config struct {
	SyncPeriod        time.Duration `yaml:"sync_period"`
	PositionsFile     string        `yaml:"filename"`
	IgnoreInvalidYaml bool          `yaml:"ignore_invalid_yaml"`
	ReadOnly          bool          `yaml:"-"`
}

// RegisterFlags with prefix registers flags where every name is prefixed by
// prefix. If prefix is a non-empty string, prefix should end with a period.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	f.DurationVar(&cfg.SyncPeriod, prefix+"positions.sync-period", 10*time.Second, "Period with this to sync the position file.")
	f.StringVar(&cfg.PositionsFile, prefix+"positions.file", "/var/log/positions.yaml", "Location to read/write positions from.")
	f.BoolVar(&cfg.IgnoreInvalidYaml, prefix+"positions.ignore-invalid-yaml", false, "whether to ignore & later overwrite positions files that are corrupted")
}

// RegisterFlags register flags.
func (cfg *Config) RegisterFlags(flags *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix("", flags)
}

// Positions tracks how far through each file we've read.
type positions struct {
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

type Positions interface {
	// GetString returns how far we've through a file as a string.
	// JournalTarget writes a journal cursor to the positions file, while
	// FileTarget writes an integer offset. Use Get to read the integer
	// offset.
	GetString(path string) string
	// Get returns how far we've read through a file. Returns an error
	// if the value stored for the file is not an integer.
	Get(path string) (int64, error)
	// PutString records (asynchronously) how far we've read through a file.
	// Unlike Put, it records a string offset and is only useful for
	// JournalTargets which doesn't have integer offsets.
	PutString(path string, pos string)
	// Put records (asynchronously) how far we've read through a file.
	Put(path string, pos int64)
	// Remove removes the position tracking for a filepath
	Remove(path string)
	// SyncPeriod returns how often the positions file gets resynced
	SyncPeriod() time.Duration
	// Stop the Position tracker.
	Stop()
}

// New makes a new Positions.
func New(logger log.Logger, cfg Config) (Positions, error) {
	positionData, err := readPositionsFile(cfg, logger)
	if err != nil {
		return nil, err
	}

	p := &positions{
		logger:    logger,
		cfg:       cfg,
		positions: positionData,
		quit:      make(chan struct{}),
		done:      make(chan struct{}),
	}

	go p.run()
	return p, nil
}

func (p *positions) Stop() {
	close(p.quit)
	<-p.done
}

func (p *positions) PutString(path string, pos string) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.positions[path] = pos
}

func (p *positions) Put(path string, pos int64) {
	p.PutString(path, strconv.FormatInt(pos, 10))
}

func (p *positions) GetString(path string) string {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	return p.positions[path]
}

func (p *positions) Get(path string) (int64, error) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	pos, ok := p.positions[path]
	if !ok {
		return 0, nil
	}
	return strconv.ParseInt(pos, 10, 64)
}

func (p *positions) Remove(path string) {
	p.mtx.Lock()
	defer p.mtx.Unlock()
	p.remove(path)
}

func (p *positions) remove(path string) {
	delete(p.positions, path)
}

func (p *positions) SyncPeriod() time.Duration {
	return p.cfg.SyncPeriod
}

func (p *positions) run() {
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

func (p *positions) save() {
	if p.cfg.ReadOnly {
		return
	}
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

func (p *positions) cleanup() {
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

func readPositionsFile(cfg Config, logger log.Logger) (map[string]string, error) {

	cleanfn := filepath.Clean(cfg.PositionsFile)
	buf, err := ioutil.ReadFile(cleanfn)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, err
	}

	var p File
	err = yaml.UnmarshalStrict(buf, &p)
	if err != nil {
		// return empty if cfg option enabled
		if cfg.IgnoreInvalidYaml {
			level.Debug(logger).Log("msg", "ignoring invalid positions file", "file", cleanfn, "error", err)
			return map[string]string{}, nil
		}

		return nil, fmt.Errorf("invalid yaml positions file [%s]: %v", cleanfn, err)
	}

	// p.Positions will be nil if the file exists but is empty
	if p.Positions == nil {
		p.Positions = map[string]string{}
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

	target := filepath.Clean(filename)
	temp := target + "-new"

	err = ioutil.WriteFile(temp, buf, os.FileMode(positionFileMode))
	if err != nil {
		return err
	}

	return os.Rename(temp, target)
}

package concat

import (
    "fmt"
    "github.com/go-kit/kit/log"
    "github.com/go-kit/kit/log/level"
    "github.com/grafana/loki/pkg/promtail/api"
    "github.com/prometheus/common/model"
    "regexp"
    "sync"
    "time"
)

type Config struct {
    // Empty string results in disabling
    MultilineStartRegexpString   string        `yaml:"multiline_start_regexp`
    // 0 for wait forever, else duration in seconds
    Timeout						 time.Duration `yaml:"flush_timeout"`
}

type logEntry struct {
    labels model.LabelSet
    time   time.Time
    line   string
    mutex  sync.Mutex
}

type Concat struct {
    next                    api.EntryHandler
    logger                  log.Logger
    multilineStartRegexp    *regexp.Regexp
    cfg                     Config
    bufferMutex             sync.Mutex
    // Map of filename to log entry
    buffer                  map[string]*logEntry
    quit                    chan struct{}
    wg                      sync.WaitGroup
}

func New(logger log.Logger, next api.EntryHandler, cfg Config) (*Concat, error) {
    multilineStartRegexp, err := regexp.Compile(cfg.MultilineStartRegexpString)
    if err != nil {
        return nil, err
    }
    c := &Concat {
        next:                   next,
        logger:                 logger,
        multilineStartRegexp:   multilineStartRegexp,
        cfg:                    cfg,
        bufferMutex:            sync.Mutex{},
        buffer:                 map[string]*logEntry{},
        quit:                   make(chan struct{}),
    }
    c.wg.Add(1)
    return c, nil
}

// Handle implement EntryHandler
func (c *Concat) Handle(labels model.LabelSet, t time.Time, line string) error {
    // Disabled concat if regex is empty
    if c.cfg.MultilineStartRegexpString == "" {
        return c.next.Handle(labels, t, line)
    }

    filenameLabel, ok := labels[api.FilenameLabel]
    if !ok {
        return fmt.Errorf("unable to find %s label in message", api.FilenameLabel)
    }
    filename := string(filenameLabel)

    c.bufferMutex.Lock()
    existingMessage, messageExists := c.buffer[filename]
    shouldFlush := c.multilineStartRegexp.MatchString(line)

    if !shouldFlush && !messageExists {
        level.Warn(c.logger).Log("msg", "encountered non-multiline-starting line in empty buffer, indicates tailer started in middle of multiline entry", "file", filename)
    }

    // If the buffer exists and we shouldn't flush, we should append to the existing buffer
    if messageExists && !shouldFlush {
        c.buffer[filename].line += "\n" + line
    } else {
        c.buffer[filename] = &logEntry {
            labels: labels,
            time:   t,
            line:   line,
        }
    }
    // At this point, we're okay to unlock the mutex.
    c.bufferMutex.Unlock()

    // Flush the old entry if necessary
    if shouldFlush && messageExists {
        return c.next.Handle(existingMessage.labels, existingMessage.time, existingMessage.line)
    }

    return nil
}

func (c *Concat) flushLoop(forceFlush bool) {
    c.bufferMutex.Lock()
    for filename, logEntry := range c.buffer {
        // Check whether the log entry should be force-flushed
        if forceFlush || time.Now().Sub(logEntry.time) > c.cfg.Timeout {
            c.next.Handle(logEntry.labels, logEntry.time, logEntry.line)
        }
        delete(c.buffer, filename)
    }
    c.bufferMutex.Unlock()
}

func (c *Concat) Run() {
    defer func() {
        c.flushLoop(true)
        c.wg.Done()
    }()
    // Set up timer loop to poll buffer every second
    timer := time.NewTicker(1*time.Second)
    for {
        select {
            case <-timer.C:
                c.flushLoop(false)
            case <-c.quit:
                return
        }
    }
}

func (c *Concat) Stop() {
    close(c.quit)
    c.wg.Wait()
}

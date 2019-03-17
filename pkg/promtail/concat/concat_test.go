package concat

import (
    "github.com/go-kit/kit/log"
    "github.com/go-kit/kit/log/level"
    "github.com/grafana/loki/pkg/promtail/api"
    "github.com/prometheus/common/model"
    "github.com/stretchr/testify/assert"
    "os"
    "sync"
    "testing"
    "time"
)

type testCase struct {
    name        string
    config      Config
    input       map[string][]string
    output      map[string][]string
    shortWait   bool
}

var disabled = Config {
    MultilineStartRegexpString: "",
}

var enabled = Config {
    MultilineStartRegexpString: "\\d{2}:\\d{2}:\\d{2}",
    Timeout: time.Duration(1 * time.Second),
}

var testCases = []testCase {
    {
        name: "disabled concat",
        config: disabled,
        input: map[string][]string {
            "path1": {
                "path1 line 1",
                "path1 line 2",
            },
            "path2": {
                "path2 line 1",
                "path2 line 2",
            },
        },
        output: map[string][]string {
            "path1": {
                "path1 line 1",
                "path1 line 2",
            },
            "path2": {
                "path2 line 1",
                "path2 line 2",
            },
        },
    }, {
        name: "base concat case",
        config: enabled,
        input: map[string][]string {
            "path1": {
                "00:00:00 path1 line 1",
                "path1 subline 1",
                "00:00:00 path1 line 2",
            },
            "path2": {
                "00:00:00 path2 line 1",
                "00:00:00 path2 line 2",
            },
        },
        output: map[string][]string {
            "path1": {
                "00:00:00 path1 line 1\npath1 subline 1",
                "00:00:00 path1 line 2",
            },
            "path2": {
                "00:00:00 path2 line 1",
                "00:00:00 path2 line 2",
            },
        },
        shortWait: false,
    }, {
        name: "base concat case",
        config: enabled,
        input: map[string][]string {
            "path1": {
                "00:00:00 path1 line 1",
                "path1 subline 1",
                "00:00:00 path1 line 2",
            },
            "path2": {
                "00:00:00 path2 line 1",
                "00:00:00 path2 line 2",
            },
        },
        output: map[string][]string {
            "path1": {
                "00:00:00 path1 line 1\npath1 subline 1",
                "00:00:00 path1 line 2",
            },
            "path2": {
                "00:00:00 path2 line 1",
                "00:00:00 path2 line 2",
            },
        },
        shortWait: false,
    }, {
        name: "test that message buffers if no timeout",
        config: enabled,
        input: map[string][]string {
            "path1": {
                "00:00:00 path1 line 1",
                "path1 subline 1",
            },
            "path2": {
                "00:00:00 path2 line 1",
                "00:00:00 path2 line 2",
            },
        },
        output: map[string][]string {
            // Everything is buffered in path1, and path2 gets the first message through
            "path2": {
                "00:00:00 path2 line 1",
            },
        },
        shortWait: true,
    },
}

type testHandler struct {
    receivedMap map[string][]string
    mutex       sync.Mutex
}

func (h *testHandler) Handle(labels model.LabelSet, time time.Time, entry string) error {
    filename := string(labels[api.FilenameLabel])
    h.mutex.Lock()
    lines, ok := h.receivedMap[filename]
    if !ok {
        lines = []string{}
    }
    h.receivedMap[filename] = append(lines, entry)
    h.mutex.Unlock()
    return nil
}

func feed(wg *sync.WaitGroup, concat *Concat, filename string, lines []string) error {
    defer wg.Done()

    labels := model.LabelSet {
        api.FilenameLabel: model.LabelValue(filename),
    }
    for _, line := range lines {
        err := concat.Handle(labels, time.Now(), line)
        if err != nil {
            return err
        }
    }
    return nil
}

func Test(t *testing.T) {
    w := log.NewSyncWriter(os.Stderr)
    logger := log.NewLogfmtLogger(w)
    logger = level.NewFilter(logger, level.AllowInfo())

    for _, testCase := range testCases {
        handler := &testHandler{
            receivedMap: map[string][]string{},
            mutex: sync.Mutex{},
        }
        concat, err := New(logger, handler, testCase.config)
        if err != nil {
            t.Fatal("Unexpected concat initialization error for test case \"", testCase.name, "\"\nerror", err)
        }
        go concat.Run()

        var wg sync.WaitGroup
        wg.Add(len(testCase.input))
        for filename, lines := range testCase.input {
            go feed(&wg, concat, filename, lines)
        }
        wg.Wait()

        if !testCase.shortWait {
            // Wait for flush interval + 1 second
            time.Sleep(testCase.config.Timeout + 1 * time.Second)
        }

        assert.Equal(t, testCase.output, handler.receivedMap, "Lines don't match in test case \"%s\"", testCase.name)
        concat.Stop()
    }
}

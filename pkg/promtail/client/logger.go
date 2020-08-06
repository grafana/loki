package client

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/fatih/color"
	"github.com/go-kit/kit/log"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"

	lokiflag "github.com/grafana/loki/pkg/util/flagext"
)

var (
	yellow = color.New(color.FgYellow)
	blue   = color.New(color.FgBlue)
)

func init() {
	if runtime.GOOS == "windows" {
		yellow.DisableColor()
		blue.DisableColor()
	}
}

type logger struct {
	*tabwriter.Writer
	sync.Mutex
}

// NewLogger creates a new client logger that logs entries instead of sending them.
func NewLogger(log log.Logger, externalLabels lokiflag.LabelSet, cfgs ...Config) (Client, error) {
	// make sure the clients config is valid
	c, err := NewMulti(log, externalLabels, cfgs...)
	if err != nil {
		return nil, err
	}
	c.Stop()

	fmt.Println(yellow.Sprint("Clients configured:"))
	for _, cfg := range cfgs {
		yaml, err := yaml.Marshal(cfg)
		if err != nil {
			return nil, err
		}
		fmt.Println("----------------------")
		fmt.Println(string(yaml))
	}
	return &logger{
		Writer: tabwriter.NewWriter(os.Stdout, 0, 8, 0, '\t', 0),
	}, nil
}

func (*logger) Stop() {}

func (l *logger) Handle(labels model.LabelSet, time time.Time, entry string) error {
	l.Lock()
	defer l.Unlock()
	fmt.Fprint(l.Writer, blue.Sprint(time.Format("2006-01-02T15:04:05")))
	fmt.Fprint(l.Writer, "\t")
	fmt.Fprint(l.Writer, yellow.Sprint(labels.String()))
	fmt.Fprint(l.Writer, "\t")
	fmt.Fprint(l.Writer, entry)
	fmt.Fprint(l.Writer, "\n")
	l.Flush()
	return nil
}

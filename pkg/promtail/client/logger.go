package client

import (
	"fmt"
	"os"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/fatih/color"
	"github.com/prometheus/common/model"
	"gopkg.in/yaml.v2"
)

type logger struct {
	*tabwriter.Writer
	sync.Mutex
}

// NewLogger creates a new client logger that logs entries instead of sending them.
func NewLogger(cfgs ...Config) (Client, error) {
	// make sure the clients config is valid
	c, err := NewMulti(util.Logger, cfgs...)
	if err != nil {
		return nil, err
	}
	c.Stop()
	fmt.Println(color.YellowString("Clients configured:"))
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
	fmt.Fprint(l.Writer, color.BlueString(time.Format("2006-01-02T15:04:05")))
	fmt.Fprint(l.Writer, "\t")
	fmt.Fprint(l.Writer, color.YellowString(labels.String()))
	fmt.Fprint(l.Writer, "\t")
	fmt.Fprint(l.Writer, entry)
	fmt.Fprint(l.Writer, "\n")
	l.Flush()
	return nil
}

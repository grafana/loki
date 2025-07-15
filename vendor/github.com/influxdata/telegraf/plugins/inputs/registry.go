package inputs

import "github.com/influxdata/telegraf"

type Creator func() telegraf.Input

var Inputs = make(map[string]Creator)

func Add(name string, creator Creator) {
	Inputs[name] = creator
}

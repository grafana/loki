package inputs

import "github.com/influxdata/telegraf"

// Creator is a function type that creates a new instance of a telegraf.Input.
type Creator func() telegraf.Input

// Inputs is a map that holds all registered input plugins by their name.
var Inputs = make(map[string]Creator)

// Add registers a new input plugin with the given name and creator function.
func Add(name string, creator Creator) {
	Inputs[name] = creator
}

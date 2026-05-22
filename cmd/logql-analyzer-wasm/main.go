//go:build js && wasm

package main

import (
	"encoding/json"
	"syscall/js"

	"github.com/grafana/loki/v3/pkg/logqlanalyzer"
)

func main() {
	done := make(chan struct{})
	js.Global().Set("analyzeLogQL", js.FuncOf(analyzeLogQL))
	<-done // block forever; keeps the Go runtime alive in the browser
}

// errJSON returns a JSON string {"error": "..."} for error responses.
// script.js calls JSON.parse() on the return value, so all returns must be JSON strings.
func errJSON(msg string) interface{} {
	b, _ := json.Marshal(map[string]string{"error": msg})
	return js.ValueOf(string(b))
}

// analyzeLogQL is exposed to JavaScript as window.analyzeLogQL(query, logsArray).
// args[0]: query string (e.g. `{job="analyze"} | logfmt`)
// args[1]: JavaScript Array of log line strings
// Returns: JSON string of logqlanalyzer.Result, or JSON {"error": "..."} on failure.
func analyzeLogQL(this js.Value, args []js.Value) interface{} {
	if len(args) < 2 {
		return errJSON("expected 2 arguments: query (string) and logs (array)")
	}
	query := args[0].String()
	logsJS := args[1]
	if logsJS.Type() != js.TypeObject {
		return errJSON("args[1] must be an array of strings")
	}
	logs := make([]string, logsJS.Length())
	for i := range logs {
		logs[i] = logsJS.Index(i).String()
	}
	result, err := logqlanalyzer.Analyze(query, logs)
	if err != nil {
		return errJSON(err.Error())
	}
	b, err := json.Marshal(result)
	if err != nil {
		return errJSON("internal: failed to serialize result: " + err.Error())
	}
	return js.ValueOf(string(b))
}

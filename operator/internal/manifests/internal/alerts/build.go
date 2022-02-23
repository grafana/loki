package alerts

import _ "embed" // https://pkg.go.dev/embed#hdr-Strings_and_Bytes

//go:embed prometheus_alerts.yaml
var alerts []byte

// Build builds prometheus alerts for loki stack
func Build() ([]byte, error) {
	return alerts, nil
}

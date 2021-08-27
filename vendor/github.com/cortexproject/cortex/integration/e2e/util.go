package e2e

import (
	"context"
	"io/ioutil"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/prompb"
)

func RunCommandAndGetOutput(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

func RunCommandWithTimeoutAndGetOutput(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.CombinedOutput()
}

func EmptyFlags() map[string]string {
	return map[string]string{}
}

func MergeFlags(inputs ...map[string]string) map[string]string {
	output := MergeFlagsWithoutRemovingEmpty(inputs...)

	for k, v := range output {
		if v == "" {
			delete(output, k)
		}
	}

	return output
}

func MergeFlagsWithoutRemovingEmpty(inputs ...map[string]string) map[string]string {
	output := map[string]string{}

	for _, input := range inputs {
		for name, value := range input {
			output[name] = value
		}
	}

	return output
}

func BuildArgs(flags map[string]string) []string {
	args := make([]string, 0, len(flags))

	for name, value := range flags {
		if value != "" {
			args = append(args, name+"="+value)
		} else {
			args = append(args, name)
		}
	}

	return args
}

func GetRequest(url string) (*http.Response, error) {
	const timeout = 1 * time.Second

	client := &http.Client{Timeout: timeout}
	return client.Get(url)
}

func PostRequest(url string) (*http.Response, error) {
	const timeout = 1 * time.Second

	client := &http.Client{Timeout: timeout}
	return client.Post(url, "", strings.NewReader(""))
}

// TimeToMilliseconds returns the input time as milliseconds, using the same
// formula used by Prometheus in order to get the same timestamp when asserting
// on query results. The formula we're mimicking here is Prometheus parseTime().
// See: https://github.com/prometheus/prometheus/blob/df80dc4d3970121f2f76cba79050983ffb3cdbb0/web/api/v1/api.go#L1690-L1694
func TimeToMilliseconds(t time.Time) int64 {
	// Convert to seconds.
	sec := float64(t.Unix()) + float64(t.Nanosecond())/1e9

	// Parse seconds.
	s, ns := math.Modf(sec)

	// Round nanoseconds part.
	ns = math.Round(ns*1000) / 1000

	// Convert to millis.
	return (int64(s) * 1e3) + (int64(ns * 1e3))
}

func GenerateSeries(name string, ts time.Time, additionalLabels ...prompb.Label) (series []prompb.TimeSeries, vector model.Vector) {
	tsMillis := TimeToMilliseconds(ts)
	value := rand.Float64()

	lbls := append(
		[]prompb.Label{
			{Name: labels.MetricName, Value: name},
		},
		additionalLabels...,
	)

	// Generate the series
	series = append(series, prompb.TimeSeries{
		Labels: lbls,
		Samples: []prompb.Sample{
			{Value: value, Timestamp: tsMillis},
		},
	})

	// Generate the expected vector when querying it
	metric := model.Metric{}
	metric[labels.MetricName] = model.LabelValue(name)
	for _, lbl := range additionalLabels {
		metric[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
	}

	vector = append(vector, &model.Sample{
		Metric:    metric,
		Value:     model.SampleValue(value),
		Timestamp: model.Time(tsMillis),
	})

	return
}

// GetTempDirectory creates a temporary directory for shared integration
// test files, either in the working directory or a directory referenced by
// the E2E_TEMP_DIR environment variable
func GetTempDirectory() (string, error) {
	var (
		dir string
		err error
	)
	// If a temp dir is referenced, return that
	if os.Getenv("E2E_TEMP_DIR") != "" {
		dir = os.Getenv("E2E_TEMP_DIR")
	} else {
		dir, err = os.Getwd()
		if err != nil {
			return "", err
		}
	}

	tmpDir, err := ioutil.TempDir(dir, "e2e_integration_test")
	if err != nil {
		return "", err
	}
	absDir, err := filepath.Abs(tmpDir)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", err
	}

	return absDir, nil
}

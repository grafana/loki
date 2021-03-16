package internal

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strconv"
	"time"

	"github.com/onsi/gomega/gbytes"
	"github.com/onsi/gomega/gexec"

	"sigs.k8s.io/controller-runtime/pkg/internal/testing/integration/addr"
)

// ProcessState define the state of the process.
type ProcessState struct {
	DefaultedProcessInput
	Session *gexec.Session
	// Healthcheck Endpoint. If we get http.StatusOK from this endpoint, we
	// assume the process is ready to operate. E.g. "/healthz". If this is set,
	// we ignore StartMessage.
	HealthCheckEndpoint string
	// HealthCheckPollInterval is the interval which will be used for polling the
	// HealthCheckEndpoint.
	// If left empty it will default to 100 Milliseconds.
	HealthCheckPollInterval time.Duration
	// StartMessage is the message to wait for on stderr. If we receive this
	// message, we assume the process is ready to operate. Ignored if
	// HealthCheckEndpoint is specified.
	//
	// The usage of StartMessage is discouraged, favour HealthCheckEndpoint
	// instead!
	//
	// Deprecated: Use HealthCheckEndpoint in favour of StartMessage
	StartMessage string
	Args         []string

	// ready holds wether the process is currently in ready state (hit the ready condition) or not.
	// It will be set to true on a successful `Start()` and set to false on a successful `Stop()`
	ready bool
}

// DefaultedProcessInput defines the default process input required to perform the test.
type DefaultedProcessInput struct {
	URL              url.URL
	Dir              string
	DirNeedsCleaning bool
	Path             string
	StopTimeout      time.Duration
	StartTimeout     time.Duration
}

// DoDefaulting sets the default configuration according to the data informed and return an DefaultedProcessInput
// and an error if some requirement was not informed.
func DoDefaulting(
	name string,
	listenURL *url.URL,
	dir string,
	path string,
	startTimeout time.Duration,
	stopTimeout time.Duration,
) (DefaultedProcessInput, error) {
	defaults := DefaultedProcessInput{
		Dir:          dir,
		Path:         path,
		StartTimeout: startTimeout,
		StopTimeout:  stopTimeout,
	}

	if listenURL == nil {
		port, host, err := addr.Suggest("")
		if err != nil {
			return DefaultedProcessInput{}, err
		}
		defaults.URL = url.URL{
			Scheme: "http",
			Host:   net.JoinHostPort(host, strconv.Itoa(port)),
		}
	} else {
		defaults.URL = *listenURL
	}

	if dir == "" {
		newDir, err := ioutil.TempDir("", "k8s_test_framework_")
		if err != nil {
			return DefaultedProcessInput{}, err
		}
		defaults.Dir = newDir
		defaults.DirNeedsCleaning = true
	}

	if path == "" {
		if name == "" {
			return DefaultedProcessInput{}, fmt.Errorf("must have at least one of name or path")
		}
		defaults.Path = BinPathFinder(name)
	}

	if startTimeout == 0 {
		defaults.StartTimeout = 20 * time.Second
	}

	if stopTimeout == 0 {
		defaults.StopTimeout = 20 * time.Second
	}

	return defaults, nil
}

type stopChannel chan struct{}

// Start starts the apiserver, waits for it to come up, and returns an error,
// if occurred.
func (ps *ProcessState) Start(stdout, stderr io.Writer) (err error) {
	if ps.ready {
		return nil
	}

	command := exec.Command(ps.Path, ps.Args...)

	ready := make(chan bool)
	timedOut := time.After(ps.StartTimeout)
	var pollerStopCh stopChannel

	if ps.HealthCheckEndpoint != "" {
		healthCheckURL := ps.URL
		healthCheckURL.Path = ps.HealthCheckEndpoint
		pollerStopCh = make(stopChannel)
		go pollURLUntilOK(healthCheckURL, ps.HealthCheckPollInterval, ready, pollerStopCh)
	} else {
		startDetectStream := gbytes.NewBuffer()
		ready = startDetectStream.Detect(ps.StartMessage)
		stderr = safeMultiWriter(stderr, startDetectStream)
	}

	ps.Session, err = gexec.Start(command, stdout, stderr)
	if err != nil {
		return err
	}

	select {
	case <-ready:
		ps.ready = true
		return nil
	case <-timedOut:
		if pollerStopCh != nil {
			close(pollerStopCh)
		}
		if ps.Session != nil {
			ps.Session.Terminate()
		}
		return fmt.Errorf("timeout waiting for process %s to start", path.Base(ps.Path))
	}
}

func safeMultiWriter(writers ...io.Writer) io.Writer {
	safeWriters := []io.Writer{}
	for _, w := range writers {
		if w != nil {
			safeWriters = append(safeWriters, w)
		}
	}
	return io.MultiWriter(safeWriters...)
}

func pollURLUntilOK(url url.URL, interval time.Duration, ready chan bool, stopCh stopChannel) {
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	for {
		res, err := http.Get(url.String())
		if err == nil {
			res.Body.Close()
			if res.StatusCode == http.StatusOK {
				ready <- true
				return
			}
		}

		select {
		case <-stopCh:
			return
		default:
			time.Sleep(interval)
		}
	}
}

// Stop stops this process gracefully, waits for its termination, and cleans up
// the CertDir if necessary.
func (ps *ProcessState) Stop() error {
	if ps.Session == nil {
		return nil
	}

	// gexec's Session methods (Signal, Kill, ...) do not check if the Process is
	// nil, so we are doing this here for now.
	// This should probably be fixed in gexec.
	if ps.Session.Command.Process == nil {
		return nil
	}

	detectedStop := ps.Session.Terminate().Exited
	timedOut := time.After(ps.StopTimeout)

	select {
	case <-detectedStop:
		break
	case <-timedOut:
		return fmt.Errorf("timeout waiting for process %s to stop", path.Base(ps.Path))
	}
	ps.ready = false
	if ps.DirNeedsCleaning {
		return os.RemoveAll(ps.Dir)
	}

	return nil
}

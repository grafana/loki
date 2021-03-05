package integration

import (
	"io"
	"time"

	"net/url"

	"sigs.k8s.io/controller-runtime/pkg/internal/testing/integration/internal"
)

// Etcd knows how to run an etcd server.
type Etcd struct {
	// URL is the address the Etcd should listen on for client connections.
	//
	// If this is not specified, we default to a random free port on localhost.
	URL *url.URL

	// Path is the path to the etcd binary.
	//
	// If this is left as the empty string, we will attempt to locate a binary,
	// by checking for the TEST_ASSET_ETCD environment variable, and the default
	// test assets directory. See the "Binaries" section above (in doc.go) for
	// details.
	Path string

	// Args is a list of arguments which will passed to the Etcd binary. Before
	// they are passed on, the`y will be evaluated as go-template strings. This
	// means you can use fields which are defined and exported on this Etcd
	// struct (e.g. "--data-dir={{ .Dir }}").
	// Those templates will be evaluated after the defaulting of the Etcd's
	// fields has already happened and just before the binary actually gets
	// started. Thus you have access to calculated fields like `URL` and others.
	//
	// If not specified, the minimal set of arguments to run the Etcd will be
	// used.
	Args []string

	// DataDir is a path to a directory in which etcd can store its state.
	//
	// If left unspecified, then the Start() method will create a fresh temporary
	// directory, and the Stop() method will clean it up.
	DataDir string

	// StartTimeout, StopTimeout specify the time the Etcd is allowed to
	// take when starting and stopping before an error is emitted.
	//
	// If not specified, these default to 20 seconds.
	StartTimeout time.Duration
	StopTimeout  time.Duration

	// Out, Err specify where Etcd should write its StdOut, StdErr to.
	//
	// If not specified, the output will be discarded.
	Out io.Writer
	Err io.Writer

	processState *internal.ProcessState
}

// Start starts the etcd, waits for it to come up, and returns an error, if one
// occoured.
func (e *Etcd) Start() error {
	if e.processState == nil {
		if err := e.setProcessState(); err != nil {
			return err
		}
	}
	return e.processState.Start(e.Out, e.Err)
}

func (e *Etcd) setProcessState() error {
	var err error

	e.processState = &internal.ProcessState{}

	e.processState.DefaultedProcessInput, err = internal.DoDefaulting(
		"etcd",
		e.URL,
		e.DataDir,
		e.Path,
		e.StartTimeout,
		e.StopTimeout,
	)
	if err != nil {
		return err
	}

	e.processState.StartMessage = internal.GetEtcdStartMessage(e.processState.URL)

	e.URL = &e.processState.URL
	e.DataDir = e.processState.Dir
	e.Path = e.processState.Path
	e.StartTimeout = e.processState.StartTimeout
	e.StopTimeout = e.processState.StopTimeout

	e.processState.Args, err = internal.RenderTemplates(
		internal.DoEtcdArgDefaulting(e.Args), e,
	)
	return err
}

// Stop stops this process gracefully, waits for its termination, and cleans up
// the DataDir if necessary.
func (e *Etcd) Stop() error {
	return e.processState.Stop()
}

// EtcdDefaultArgs exposes the default args for Etcd so that you
// can use those to append your own additional arguments.
//
// The internal default arguments are explicitly copied here, we don't want to
// allow users to change the internal ones.
var EtcdDefaultArgs = append([]string{}, internal.EtcdDefaultArgs...)

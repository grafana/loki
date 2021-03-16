package integration

import (
	"fmt"
	"io"
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/internal/testing/integration/addr"
	"sigs.k8s.io/controller-runtime/pkg/internal/testing/integration/internal"
)

// APIServer knows how to run a kubernetes apiserver.
type APIServer struct {
	// URL is the address the ApiServer should listen on for client connections.
	//
	// If this is not specified, we default to a random free port on localhost.
	URL *url.URL

	// SecurePort is the additional secure port that the APIServer should listen on.
	SecurePort int

	// Path is the path to the apiserver binary.
	//
	// If this is left as the empty string, we will attempt to locate a binary,
	// by checking for the TEST_ASSET_KUBE_APISERVER environment variable, and
	// the default test assets directory. See the "Binaries" section above (in
	// doc.go) for details.
	Path string

	// Args is a list of arguments which will passed to the APIServer binary.
	// Before they are passed on, they will be evaluated as go-template strings.
	// This means you can use fields which are defined and exported on this
	// APIServer struct (e.g. "--cert-dir={{ .Dir }}").
	// Those templates will be evaluated after the defaulting of the APIServer's
	// fields has already happened and just before the binary actually gets
	// started. Thus you have access to calculated fields like `URL` and others.
	//
	// If not specified, the minimal set of arguments to run the APIServer will
	// be used.
	Args []string

	// CertDir is a path to a directory containing whatever certificates the
	// APIServer will need.
	//
	// If left unspecified, then the Start() method will create a fresh temporary
	// directory, and the Stop() method will clean it up.
	CertDir string

	// EtcdURL is the URL of the Etcd the APIServer should use.
	//
	// If this is not specified, the Start() method will return an error.
	EtcdURL *url.URL

	// StartTimeout, StopTimeout specify the time the APIServer is allowed to
	// take when starting and stoppping before an error is emitted.
	//
	// If not specified, these default to 20 seconds.
	StartTimeout time.Duration
	StopTimeout  time.Duration

	// Out, Err specify where APIServer should write its StdOut, StdErr to.
	//
	// If not specified, the output will be discarded.
	Out io.Writer
	Err io.Writer

	processState *internal.ProcessState
}

// Start starts the apiserver, waits for it to come up, and returns an error,
// if occurred.
func (s *APIServer) Start() error {
	if s.processState == nil {
		if err := s.setProcessState(); err != nil {
			return err
		}
	}
	return s.processState.Start(s.Out, s.Err)
}

func (s *APIServer) setProcessState() error {
	if s.EtcdURL == nil {
		return fmt.Errorf("expected EtcdURL to be configured")
	}

	var err error

	s.processState = &internal.ProcessState{}

	s.processState.DefaultedProcessInput, err = internal.DoDefaulting(
		"kube-apiserver",
		s.URL,
		s.CertDir,
		s.Path,
		s.StartTimeout,
		s.StopTimeout,
	)
	if err != nil {
		return err
	}

	// Defaulting the secure port
	if s.SecurePort == 0 {
		s.SecurePort, _, err = addr.Suggest("")
		if err != nil {
			return err
		}
	}

	s.processState.HealthCheckEndpoint = "/healthz"

	s.URL = &s.processState.URL
	s.CertDir = s.processState.Dir
	s.Path = s.processState.Path
	s.StartTimeout = s.processState.StartTimeout
	s.StopTimeout = s.processState.StopTimeout

	if err := s.populateAPIServerCerts(); err != nil {
		return err
	}

	s.processState.Args, err = internal.RenderTemplates(
		internal.DoAPIServerArgDefaulting(s.Args), s,
	)
	return err
}

func (s *APIServer) populateAPIServerCerts() error {
	_, statErr := os.Stat(filepath.Join(s.CertDir, "apiserver.crt"))
	if !os.IsNotExist(statErr) {
		return statErr
	}

	ca, err := internal.NewTinyCA()
	if err != nil {
		return err
	}

	certs, err := ca.NewServingCert()
	if err != nil {
		return err
	}

	certData, keyData, err := certs.AsBytes()
	if err != nil {
		return err
	}

	if err := ioutil.WriteFile(filepath.Join(s.CertDir, "apiserver.crt"), certData, 0640); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(s.CertDir, "apiserver.key"), keyData, 0640); err != nil {
		return err
	}

	return nil
}

// Stop stops this process gracefully, waits for its termination, and cleans up
// the CertDir if necessary.
func (s *APIServer) Stop() error {
	if s.processState != nil {
		return s.processState.Stop()
	}
	return nil
}

// APIServerDefaultArgs exposes the default args for the APIServer so that you
// can use those to append your own additional arguments.
//
// The internal default arguments are explicitly copied here, we don't want to
// allow users to change the internal ones.
var APIServerDefaultArgs = append([]string{}, internal.APIServerDefaultArgs...)

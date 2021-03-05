package integration

import (
	"bytes"
	"io"
	"os/exec"

	"sigs.k8s.io/controller-runtime/pkg/internal/testing/integration/internal"
)

// KubeCtl is a wrapper around the kubectl binary.
type KubeCtl struct {
	// Path where the kubectl binary can be found.
	//
	// If this is left empty, we will attempt to locate a binary, by checking for
	// the TEST_ASSET_KUBECTL environment variable, and the default test assets
	// directory. See the "Binaries" section above (in doc.go) for details.
	Path string

	// Opts can be used to configure additional flags which will be used each
	// time the wrapped binary is called.
	//
	// For example, you might want to use this to set the URL of the APIServer to
	// connect to.
	Opts []string
}

// Run executes the wrapped binary with some preconfigured options and the
// arguments given to this method. It returns Readers for the stdout and
// stderr.
func (k *KubeCtl) Run(args ...string) (stdout, stderr io.Reader, err error) {
	if k.Path == "" {
		k.Path = internal.BinPathFinder("kubectl")
	}

	stdoutBuffer := &bytes.Buffer{}
	stderrBuffer := &bytes.Buffer{}
	allArgs := append(k.Opts, args...)

	cmd := exec.Command(k.Path, allArgs...)
	cmd.Stdout = stdoutBuffer
	cmd.Stderr = stderrBuffer

	err = cmd.Run()

	return stdoutBuffer, stderrBuffer, err
}

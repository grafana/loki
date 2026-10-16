package commands

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMain removes the connection settings from the environment of the test
// runner, because the client reads them.
func TestMain(m *testing.M) {
	for _, env := range os.Environ() {
		if name, _, _ := strings.Cut(env, "="); strings.HasPrefix(name, "LOKI_") {
			os.Unsetenv(name)
		}
	}
	os.Exit(m.Run())
}

// runApplication runs the application with the arguments and returns what it printed to stdout.
func runApplication(t *testing.T, args ...string) (string, error) {
	t.Helper()

	app, global := NewApplication()
	defer global.Close()
	app.Terminate(func(code int) { t.Fatalf("application terminated with code %d", code) })

	r, w, err := os.Pipe()
	require.NoError(t, err)

	stdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = stdout }()

	out := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		out <- string(b)
	}()

	_, err = app.Parse(args)

	require.NoError(t, w.Close())
	os.Stdout = stdout
	return <-out, err
}

// request is a request received by the test server.
type request struct {
	Method string
	Path   string
	Header http.Header
	Body   string
}

// server is a test server that records requests.
type server struct {
	*httptest.Server

	mu       sync.Mutex
	requests []request
}

// newServer starts a server. The handler writes the response, the default response is 200 OK without a body.
func newServer(t *testing.T, handler http.HandlerFunc) *server {
	t.Helper()

	s := &server{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.requests = append(s.requests, request{r.Method, r.URL.EscapedPath(), r.Header.Clone(), string(b)})
		s.mu.Unlock()

		if handler != nil {
			handler(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *server) Requests() []request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]request(nil), s.requests...)
}

// captureStderr returns what f printed to stderr.
func captureStderr(t *testing.T, f func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	stderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = stderr }()

	out := make(chan string)
	go func() {
		b, _ := io.ReadAll(r)
		out <- string(b)
	}()

	f()

	require.NoError(t, w.Close())
	os.Stderr = stderr
	return <-out
}

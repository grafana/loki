package explorer

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore/providers/filesystem"
)

// newFilesystemService returns an explorer backed by the filesystem object
// store rooted at root. The filesystem backend joins request keys onto root
// without containment, which is the backend the traversal tests exercise.
func newFilesystemService(t *testing.T, root string) *Service {
	t.Helper()

	bkt, err := filesystem.NewBucket(root)
	require.NoError(t, err)

	svc, err := New(bkt, log.NewNopLogger())
	require.NoError(t, err)
	return svc
}

func TestHandlersRejectPathTraversal(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "bucket")
	require.NoError(t, os.MkdirAll(root, 0o755))

	// A secret file outside the bucket root that must never be reachable.
	require.NoError(t, os.WriteFile(filepath.Join(tmp, "secret.txt"), []byte("top secret"), 0o600))
	// A legitimate object inside the root.
	require.NoError(t, os.WriteFile(filepath.Join(root, "object.dat"), []byte("ok"), 0o600))

	svc := newFilesystemService(t, root)

	traversals := []string{"../secret.txt", "../../etc/passwd", "/etc/passwd"}

	for _, key := range traversals {
		t.Run("download "+key, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/dataobj/api/v1/download?file="+key, nil)
			svc.handleDownload(rr, req)

			require.Equal(t, http.StatusBadRequest, rr.Code)
			body, _ := io.ReadAll(rr.Result().Body)
			require.NotContains(t, string(body), "top secret")
		})

		t.Run("inspect "+key, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/dataobj/api/v1/inspect?file="+key, nil)
			svc.handleInspect(rr, req)

			require.Equal(t, http.StatusBadRequest, rr.Code)
		})

		t.Run("list "+key, func(t *testing.T) {
			rr := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/dataobj/api/v1/list?path="+key, nil)
			svc.handleList(rr, req)

			require.Equal(t, http.StatusBadRequest, rr.Code)
		})
	}
}

func TestHandlersAllowInRootKeys(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "bucket")
	require.NoError(t, os.MkdirAll(root, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "object.dat"), []byte("ok"), 0o600))

	svc := newFilesystemService(t, root)

	// A normal in-root download still works.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/dataobj/api/v1/download?file=object.dat", nil)
	svc.handleDownload(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
	body, _ := io.ReadAll(rr.Result().Body)
	require.Equal(t, "ok", string(body))

	// Listing the root (empty path) is allowed.
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/dataobj/api/v1/list", nil)
	svc.handleList(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)
}

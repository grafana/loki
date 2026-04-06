package tsdb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	shipperstorage "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/storage"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
)

type fakeIndexStorageClient struct {
	listUserFilesFn  func(ctx context.Context, tableName, userID string, bypassCache bool) ([]shipperstorage.IndexFile, error)
	getUserFileFn    func(ctx context.Context, tableName, userID, fileName string) (io.ReadCloser, error)
	isNotFoundErrFn  func(error) bool
	listTablesFn     func(ctx context.Context) ([]string, error)
	refreshTablesFn  func(ctx context.Context)
	refreshTableFn   func(ctx context.Context, tableName string)
	stopFn           func()
	listFilesFn      func(ctx context.Context, tableName string, bypassCache bool) ([]shipperstorage.IndexFile, []string, error)
	getFileFn        func(ctx context.Context, tableName, fileName string) (io.ReadCloser, error)
	putFileFn        func(ctx context.Context, tableName, fileName string, file io.Reader) error
	deleteFileFn     func(ctx context.Context, tableName, fileName string) error
	putUserFileFn    func(ctx context.Context, tableName, userID, fileName string, file io.Reader) error
	deleteUserFileFn func(ctx context.Context, tableName, userID, fileName string) error
}

func (f fakeIndexStorageClient) ListUserFiles(ctx context.Context, tableName, userID string, bypassCache bool) ([]shipperstorage.IndexFile, error) {
	if f.listUserFilesFn == nil {
		return nil, nil
	}
	return f.listUserFilesFn(ctx, tableName, userID, bypassCache)
}

func (f fakeIndexStorageClient) GetUserFile(ctx context.Context, tableName, userID, fileName string) (io.ReadCloser, error) {
	if f.getUserFileFn == nil {
		return io.NopCloser(strings.NewReader("")), nil
	}
	return f.getUserFileFn(ctx, tableName, userID, fileName)
}

func (f fakeIndexStorageClient) PutUserFile(ctx context.Context, tableName, userID, fileName string, file io.Reader) error {
	if f.putUserFileFn == nil {
		return nil
	}
	return f.putUserFileFn(ctx, tableName, userID, fileName, file)
}

func (f fakeIndexStorageClient) DeleteUserFile(ctx context.Context, tableName, userID, fileName string) error {
	if f.deleteUserFileFn == nil {
		return nil
	}
	return f.deleteUserFileFn(ctx, tableName, userID, fileName)
}

func (f fakeIndexStorageClient) ListFiles(ctx context.Context, tableName string, bypassCache bool) ([]shipperstorage.IndexFile, []string, error) {
	if f.listFilesFn == nil {
		return nil, nil, nil
	}
	return f.listFilesFn(ctx, tableName, bypassCache)
}

func (f fakeIndexStorageClient) GetFile(ctx context.Context, tableName, fileName string) (io.ReadCloser, error) {
	if f.getFileFn == nil {
		return io.NopCloser(strings.NewReader("")), nil
	}
	return f.getFileFn(ctx, tableName, fileName)
}

func (f fakeIndexStorageClient) PutFile(ctx context.Context, tableName, fileName string, file io.Reader) error {
	if f.putFileFn == nil {
		return nil
	}
	return f.putFileFn(ctx, tableName, fileName, file)
}

func (f fakeIndexStorageClient) DeleteFile(ctx context.Context, tableName, fileName string) error {
	if f.deleteFileFn == nil {
		return nil
	}
	return f.deleteFileFn(ctx, tableName, fileName)
}

func (f fakeIndexStorageClient) RefreshIndexTableNamesCache(ctx context.Context) {
	if f.refreshTablesFn != nil {
		f.refreshTablesFn(ctx)
	}
}

func (f fakeIndexStorageClient) ListTables(ctx context.Context) ([]string, error) {
	if f.listTablesFn == nil {
		return nil, nil
	}
	return f.listTablesFn(ctx)
}

func (f fakeIndexStorageClient) RefreshIndexTableCache(ctx context.Context, tableName string) {
	if f.refreshTableFn != nil {
		f.refreshTableFn(ctx, tableName)
	}
}

func (f fakeIndexStorageClient) IsFileNotFoundErr(err error) bool {
	if f.isNotFoundErrFn == nil {
		return false
	}
	return f.isNotFoundErrFn(err)
}

func (f fakeIndexStorageClient) Stop() {
	if f.stopFn != nil {
		f.stopFn()
	}
}

func TestTableNamesForRange(t *testing.T) {
	from := time.Date(2026, 3, 1, 23, 59, 0, 0, time.UTC)
	to := time.Date(2026, 3, 2, 0, 1, 0, 0, time.UTC)

	t.Run("default prefix", func(t *testing.T) {
		tables := tableNamesForRange(from, to, tsdbIndexTablePrefix)
		require.Equal(t, []string{"index_20513", "index_20514"}, tables)
	})

	t.Run("custom prefix", func(t *testing.T) {
		tables := tableNamesForRange(from, to, "loki_dev_005_tsdb_index_")
		require.Equal(t, []string{"loki_dev_005_tsdb_index_20513", "loki_dev_005_tsdb_index_20514"}, tables)
	})
}

func TestFilterTSDBFilesByTimeOverlap(t *testing.T) {
	match := tsdb.SingleTenantTSDBIdentifier{TS: time.Unix(0, 0), From: model.Time(10), Through: model.Time(20), Checksum: 0xabcd}.Name()
	miss := tsdb.SingleTenantTSDBIdentifier{TS: time.Unix(0, 0), From: model.Time(50), Through: model.Time(60), Checksum: 0xbeef}.Name()

	files := []shipperstorage.IndexFile{{Name: match}, {Name: miss}, {Name: match + ".gz"}, {Name: "not-tsdb.txt"}}

	filtered, err := filterTSDBFilesByTimeOverlap(files, model.Time(15), model.Time(30))
	require.NoError(t, err)
	require.Len(t, filtered, 2)
	require.Equal(t, match, filtered[0].ObjectName)
	require.Equal(t, match+".gz", filtered[1].ObjectName)
	for _, f := range filtered {
		require.Equal(t, model.Time(10), f.From)
		require.Equal(t, model.Time(20), f.Through)
	}
}

func TestDiscoverAndDownloadIndexes(t *testing.T) {
	origDownload := downloadFileFromStorage
	t.Cleanup(func() { downloadFileFromStorage = origDownload })

	from := time.Unix(20514*24*60*60, 0).UTC().Add(2 * time.Hour)
	to := from.Add(6 * time.Hour)

	nameA := tsdb.SingleTenantTSDBIdentifier{TS: time.Unix(0, 0), From: model.TimeFromUnixNano(from.Add(-time.Hour).UnixNano()), Through: model.TimeFromUnixNano(from.Add(time.Hour).UnixNano()), Checksum: 1}.Name()
	nameB := tsdb.SingleTenantTSDBIdentifier{TS: time.Unix(0, 0), From: model.TimeFromUnixNano(to.Add(-time.Hour).UnixNano()), Through: model.TimeFromUnixNano(to.Add(time.Hour).UnixNano()), Checksum: 2}.Name() + ".gz"
	nameOutside := tsdb.SingleTenantTSDBIdentifier{TS: time.Unix(0, 0), From: model.TimeFromUnixNano(to.Add(2 * time.Hour).UnixNano()), Through: model.TimeFromUnixNano(to.Add(3 * time.Hour).UnixNano()), Checksum: 3}.Name()

	listByTable := map[string][]shipperstorage.IndexFile{
		"index_20514": {
			{Name: nameOutside},
			{Name: nameA},
			{Name: nameB},
		},
	}

	downloaded := map[string]bool{}
	downloadFileFromStorage = func(destination string, decompressFile bool, _ bool, _ log.Logger, getFileFunc shipperstorage.GetFileFunc) error {
		rc, err := getFileFunc()
		if err != nil {
			return err
		}
		defer rc.Close()

		_, err = io.Copy(io.Discard, rc)
		if err != nil {
			return err
		}
		downloaded[destination] = decompressFile
		return nil
	}

	client := fakeIndexStorageClient{
		listUserFilesFn: func(_ context.Context, tableName, _ string, _ bool) ([]shipperstorage.IndexFile, error) {
			return listByTable[tableName], nil
		},
		getUserFileFn: func(_ context.Context, _, _, fileName string) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewBufferString(fileName)), nil
		},
	}

	result, err := DiscoverAndDownloadIndexes(context.Background(), client, DownloadConfig{
		Tenant: "tenant-a",
		From:   from,
		To:     to,
		TmpDir: t.TempDir(),
	})
	require.NoError(t, err)
	require.Len(t, result.Files, 2)

	fileA := result.Files[0]
	fileB := result.Files[1]
	require.Equal(t, "index_20514", fileA.Table)
	require.Equal(t, nameA, fileA.ObjectName)
	require.Equal(t, "index_20514", fileB.Table)
	require.Equal(t, nameB, fileB.ObjectName)
	require.True(t, strings.HasSuffix(fileA.LocalPath, nameA))
	require.True(t, strings.HasSuffix(fileB.LocalPath, strings.TrimSuffix(nameB, ".gz")))
	require.False(t, downloaded[fileA.LocalPath])
	require.True(t, downloaded[fileB.LocalPath])
}

func TestDiscoverAndDownloadIndexes_CustomTablePrefix(t *testing.T) {
	origDownload := downloadFileFromStorage
	t.Cleanup(func() { downloadFileFromStorage = origDownload })

	from := time.Unix(20514*24*60*60, 0).UTC().Add(2 * time.Hour)
	to := from.Add(6 * time.Hour)

	nameA := tsdb.SingleTenantTSDBIdentifier{TS: time.Unix(0, 0), From: model.TimeFromUnixNano(from.Add(-time.Hour).UnixNano()), Through: model.TimeFromUnixNano(from.Add(time.Hour).UnixNano()), Checksum: 1}.Name()

	const customPrefix = "loki_dev_005_tsdb_index_"

	listByTable := map[string][]shipperstorage.IndexFile{
		customPrefix + "20514": {
			{Name: nameA},
		},
	}

	downloadFileFromStorage = func(_ string, _ bool, _ bool, _ log.Logger, getFileFunc shipperstorage.GetFileFunc) error {
		rc, err := getFileFunc()
		if err != nil {
			return err
		}
		defer rc.Close()
		_, _ = io.Copy(io.Discard, rc)
		return nil
	}

	client := fakeIndexStorageClient{
		listUserFilesFn: func(_ context.Context, tableName, _ string, _ bool) ([]shipperstorage.IndexFile, error) {
			return listByTable[tableName], nil
		},
		getUserFileFn: func(_ context.Context, _, _, fileName string) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewBufferString(fileName)), nil
		},
	}

	result, err := DiscoverAndDownloadIndexes(context.Background(), client, DownloadConfig{
		Tenant:      "tenant-a",
		From:        from,
		To:          to,
		TmpDir:      t.TempDir(),
		TablePrefix: customPrefix,
	})
	require.NoError(t, err)
	require.Len(t, result.Files, 1)
	require.Equal(t, customPrefix+"20514", result.Files[0].Table)
	require.Equal(t, nameA, result.Files[0].ObjectName)
}

func TestDiscoverAndDownloadIndexes_NotFoundTable(t *testing.T) {
	notFoundErr := errors.New("not found")
	client := fakeIndexStorageClient{
		listUserFilesFn: func(_ context.Context, _, _ string, _ bool) ([]shipperstorage.IndexFile, error) {
			return nil, notFoundErr
		},
		isNotFoundErrFn: func(err error) bool {
			return errors.Is(err, notFoundErr)
		},
	}

	result, err := DiscoverAndDownloadIndexes(context.Background(), client, DownloadConfig{
		Tenant: "tenant-a",
		From:   time.Unix(20514*24*60*60, 0).UTC(),
		To:     time.Unix(20514*24*60*60+60, 0).UTC(),
		TmpDir: t.TempDir(),
	})
	require.NoError(t, err)
	require.Empty(t, result.Files)
}

func TestDiscoverAndDownloadIndexes_DownloadErrorIncludesContext(t *testing.T) {
	origDownload := downloadFileFromStorage
	t.Cleanup(func() { downloadFileFromStorage = origDownload })

	from := time.Unix(20514*24*60*60, 0).UTC()
	fileName := tsdb.SingleTenantTSDBIdentifier{TS: time.Unix(0, 0), From: model.TimeFromUnixNano(from.UnixNano()), Through: model.TimeFromUnixNano(from.Add(time.Hour).UnixNano()), Checksum: 11}.Name()

	downloadFileFromStorage = func(_ string, _ bool, _ bool, _ log.Logger, _ shipperstorage.GetFileFunc) error {
		return errors.New("boom")
	}

	client := fakeIndexStorageClient{
		listUserFilesFn: func(_ context.Context, _, _ string, _ bool) ([]shipperstorage.IndexFile, error) {
			return []shipperstorage.IndexFile{{Name: fileName}}, nil
		},
		getUserFileFn: func(_ context.Context, _, _, _ string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader("x")), nil
		},
	}

	_, err := DiscoverAndDownloadIndexes(context.Background(), client, DownloadConfig{Tenant: "tenant", From: from, To: from.Add(time.Hour), TmpDir: t.TempDir()})
	require.Error(t, err)
	require.Contains(t, err.Error(), fmt.Sprintf("download file %q", fileName))
	require.Contains(t, err.Error(), "index_20514")
}

// SPDX-License-Identifier: AGPL-3.0-only

package mimir

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/runutil"
	"github.com/pkg/errors"

	alertstorelocal "github.com/grafana/mimir/pkg/alertmanager/alertstore/local"
	rulestorelocal "github.com/grafana/mimir/pkg/ruler/rulestore/local"
	"github.com/grafana/mimir/pkg/storage/bucket"
	"github.com/grafana/mimir/pkg/util/fs"
)

var (
	errObjectStorage = "unable to successfully send a request to object storage"
)

type dirExistsFunc func(string) (bool, error)
type isDirReadWritableFunc func(dir string) error

func runSanityCheck(ctx context.Context, cfg Config, logger log.Logger) error {
	level.Info(logger).Log("msg", "Checking directories read/write access")
	if err := checkDirectoriesReadWriteAccess(cfg, fs.DirExists, fs.IsDirReadWritable); err != nil {
		level.Error(logger).Log("msg", "Unable to access directory", "err", err)
		return err
	}
	level.Info(logger).Log("msg", "Directories read/write access successfully checked")

	level.Info(logger).Log("msg", "Checking object storage config")
	if err := checkObjectStoresConfigWithRetries(ctx, cfg, logger); err != nil {
		level.Error(logger).Log("msg", "Unable to successfully connect to configured object storage", "err", err)
		return err
	}
	level.Info(logger).Log("msg", "Object storage config successfully checked")

	return nil
}

func checkDirectoriesReadWriteAccess(
	cfg Config,
	dirExistFn dirExistsFunc,
	isDirReadWritableFn isDirReadWritableFunc,
) error {
	errs := multierror.New()

	if cfg.isAnyModuleEnabled(All, Ingester, Write) {
		errs.Add(errors.Wrap(checkDirReadWriteAccess(cfg.Ingester.BlocksStorageConfig.TSDB.Dir, dirExistFn, isDirReadWritableFn), "ingester"))
	}
	if cfg.isAnyModuleEnabled(All, StoreGateway, Backend) {
		errs.Add(errors.Wrap(checkDirReadWriteAccess(cfg.BlocksStorage.BucketStore.SyncDir, dirExistFn, isDirReadWritableFn), "store-gateway"))
	}
	if cfg.isAnyModuleEnabled(All, Compactor, Backend) {
		errs.Add(errors.Wrap(checkDirReadWriteAccess(cfg.Compactor.DataDir, dirExistFn, isDirReadWritableFn), "compactor"))
	}
	if cfg.isAnyModuleEnabled(All, Ruler, Backend) {
		errs.Add(errors.Wrap(checkDirReadWriteAccess(cfg.Ruler.RulePath, dirExistFn, isDirReadWritableFn), "ruler"))
	}
	if cfg.isAnyModuleEnabled(AlertManager, Backend) {
		errs.Add(errors.Wrap(checkDirReadWriteAccess(cfg.Alertmanager.DataDir, dirExistFn, isDirReadWritableFn), "alertmanager"))
	}

	return errs.Err()
}

func checkDirReadWriteAccess(dir string, dirExistFn dirExistsFunc, isDirReadWritableFn isDirReadWritableFunc) error {
	d := dir
	for {
		exists, err := dirExistFn(d)
		if err != nil {
			return err
		}
		if exists {
			break
		}

		dNext := filepath.Dir(d)
		if d == dNext {
			break // Root path reached.
		}
		d = dNext
	}

	if err := isDirReadWritableFn(d); err != nil {
		return fmt.Errorf("failed to access directory %s: %w", dir, err)
	}
	return nil
}

func checkObjectStoresConfigWithRetries(ctx context.Context, cfg Config, logger log.Logger) (err error) {
	const maxRetries = 20

	backoff := backoff.New(ctx, backoff.Config{
		MinBackoff: time.Second,
		MaxBackoff: 5 * time.Second,
		MaxRetries: maxRetries,
	})

	for backoff.Ongoing() {
		err = checkObjectStoresConfig(ctx, cfg, logger)
		if err == nil {
			return nil
		}

		if backoff.NumRetries() < maxRetries-1 {
			level.Warn(logger).Log("msg", "Unable to successfully connect to configured object storage (will retry)", "err", err)
		} else {
			level.Warn(logger).Log("msg", "Unable to successfully connect to configured object storage", "err", err)
		}

		backoff.Wait()
	}

	return err
}

func checkObjectStoresConfig(ctx context.Context, cfg Config, logger log.Logger) error {
	errs := multierror.New()

	// Check blocks storage config only if running at least one component using it.
	if cfg.isAnyModuleEnabled(All, Ingester, Querier, Ruler, StoreGateway, Compactor, Write, Read, Backend) {
		errs.Add(errors.Wrap(checkObjectStoreConfig(ctx, cfg.BlocksStorage.Bucket, logger), "blocks storage"))
	}

	// Check alertmanager storage config.
	if cfg.isAnyModuleEnabled(AlertManager, Backend) && cfg.AlertmanagerStorage.Backend != alertstorelocal.Name {
		errs.Add(errors.Wrap(checkObjectStoreConfig(ctx, cfg.AlertmanagerStorage.Config, logger), "alertmanager storage"))
	}

	// Check ruler storage config.
	if cfg.isAnyModuleEnabled(All, Ruler, Backend) && cfg.RulerStorage.Backend != rulestorelocal.Name {
		errs.Add(errors.Wrap(checkObjectStoreConfig(ctx, cfg.RulerStorage.Config, logger), "ruler storage"))
	}

	return errs.Err()
}

func checkObjectStoreConfig(ctx context.Context, cfg bucket.Config, logger log.Logger) error {
	// Hardcoded but relatively high timeout.
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	client, err := bucket.NewClient(ctx, cfg, "sanity-check", logger, nil)
	if err != nil {
		return errors.Wrap(err, errObjectStorage)
	}

	defer func() {
		// Ensure the client gets closed. Ignore the returned error here.
		_ = client.Close()
	}()

	// Read an object we don't expect to exist, just to check if the object storage
	// replies with the expected error.
	reader, err := client.Get(ctx, "sanity-check-at-startup")
	if err != nil {
		if client.IsObjNotFoundErr(err) {
			return nil
		}
		return errors.Wrap(err, errObjectStorage)
	}

	runutil.ExhaustCloseWithErrCapture(&err, reader, "error while reading from object store")
	return err
}

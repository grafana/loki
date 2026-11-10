package metastore

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"golang.org/x/sync/errgroup"

	utillog "github.com/grafana/loki/v3/pkg/util/log"
	serverutil "github.com/grafana/loki/v3/pkg/util/server"
)

// recoverPanicAsError logs a panic of the calling function, and sets *err to an error that carries
// the panic value. The error names the index object when indexPath is not empty. It does nothing
// when the calling function does not panic.
//
// Call it with defer, as the first statement of a function that reads an index object and runs on a
// goroutine of its own.
func recoverPanicAsError(ctx context.Context, logger log.Logger, indexPath string, err *error) {
	// recover only works when the deferred function calls it, so this function is the deferred one.
	//
	// A goroutine has no caller that can recover its panic, and the panic would end the process. This
	// includes the function that a singleflight call runs: the library raises its panic again on a
	// new goroutine, where nothing can recover it.
	panicked := recover()
	if panicked == nil {
		return
	}

	serverutil.RecordPanic()
	level.Error(utillog.WithContext(ctx, logger)).Log(
		"msg", "panic reading the sections of an index object",
		"index", indexPath,
		"panic", panicked,
		"stack", string(debug.Stack()),
	)

	if indexPath == "" {
		*err = fmt.Errorf("reading the sections of an index object: %v", panicked)
		return
	}
	*err = fmt.Errorf("reading the sections of index object %q: %v", indexPath, panicked)
}

// goRecovering starts fn on a goroutine of the group. A panic in fn becomes an error of the group.
// The error does not name the index object.
func goRecovering(ctx context.Context, group *errgroup.Group, fn func() error) {
	group.Go(func() (err error) {
		defer recoverPanicAsError(ctx, utillog.Logger, "", &err)
		return fn()
	})
}

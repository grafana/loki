package watcher

import (
	"fmt"
	"os"

	"github.com/pkg/errors"

	"github.com/unchartedsoftware/witch/glob"
)

const (
	// Added represents a file creation event.
	Added = "added"
	// Changed represents a file change event.
	Changed = "changed"
	// Removed represents a file removal event.
	Removed = "removed"
)

// Watcher represents a simple struct for scanning and checking for any changes
// that occur in a set of watched files and directories.
type Watcher struct {
	watches []string
	ignores []string
	prev    map[string]os.FileInfo
}

// Event represents a single detected file event.
type Event struct {
	Type string
	Path string
}

// New instantiates and returns a new watcher struct.
func New() *Watcher {
	return &Watcher{}
}

// Watch adds a single file, directory, or glob to the file watch list.
func (w *Watcher) Watch(arg string) {
	w.watches = append(w.watches, arg)
}

// Ignore adds a single file, directory, or glob to the file ignore list.
func (w *Watcher) Ignore(arg string) {
	w.ignores = append(w.ignores, arg)
}

// ScanForEvents returns any events that occurred since the last scan.
func (w *Watcher) ScanForEvents() ([]Event, error) {
	// get all current watches
	targets, err := w.scan()
	if err != nil {
		return nil, err
	}
	// check any events
	return w.check(targets), nil
}

// NumTargets returns the number of currently watched targets.
func (w *Watcher) NumTargets() (uint64, error) {
	// get all current watches
	targets, err := w.scan()
	if err != nil {
		return 0, err
	}
	// scan for current status
	return uint64(len(targets)), nil
}

func (w *Watcher) scanIgnores(args []string) ([]string, error) {
	var ignores []string
	for _, arg := range args {
		// expand the glob
		files, err := glob.Glob(nil, arg, nil, false)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("unable to expand glob %s", arg))
		}
		// merge ignores
		for path := range files {
			ignores = append(ignores, path)
		}
	}
	return ignores, nil
}

func (w *Watcher) scanWatches(args []string, ignores []string) (map[string]os.FileInfo, error) {
	watches := make(map[string]os.FileInfo)
	for _, arg := range args {
		// expand the glob
		_, err := glob.Glob(watches, arg, ignores, true)
		if err != nil {
			return nil, errors.Wrap(err, fmt.Sprintf("unable to expand glob %s", arg))
		}
	}
	return watches, nil
}

func (w *Watcher) scan() (map[string]os.FileInfo, error) {
	// scan for ignores
	ignores, err := w.scanIgnores(w.ignores)
	if err != nil {
		return nil, err
	}
	// scan for watches
	return w.scanWatches(w.watches, ignores)
}

func (w *Watcher) check(latest map[string]os.FileInfo) []Event {
	if w.prev == nil {
		w.prev = latest
		return nil
	}
	var events []Event
	// for each current file, see if it is new, or has changed since prev scan
	for path, file := range latest {
		prev, ok := w.prev[path]
		if !ok {
			// new file
			events = append(events, Event{
				Type: Added,
				Path: path,
			})
		} else if !prev.ModTime().Equal(file.ModTime()) {
			// changed file
			events = append(events, Event{
				Type: Changed,
				Path: path,
			})
		}
		// remove from prev
		delete(w.prev, path)
	}
	// iterate over remaining prev files, as they no longer exist
	for path := range w.prev {
		// removed file
		events = append(events, Event{
			Type: Removed,
			Path: path,
		})
	}
	// store latest as prev for next iteration
	w.prev = latest
	return events
}

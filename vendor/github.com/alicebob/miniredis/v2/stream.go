// Basic stream implementation.

package miniredis

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

var (
	errInvalidStreamValue = errors.New("stream id is not bigger than the top item")
	errZeroStreamValue    = errors.New("stream id is 0-0")
)

type streamKey []StreamEntry

// A StreamEntry is an entry in a stream. The ID is always of the form
// "123-123". Values should have an even length of entries.
type StreamEntry struct {
	ID     string
	Values []string
}

type streamGroupKey map[string]streamGroupEntry

type streamGroupEntry struct {
	lastID  string
	pending []pendingEntry
}

type pendingEntry struct {
	consumer string
	ID       string
}

func (ss *streamKey) generateID(now time.Time) string {
	ts := uint64(now.UnixNano()) / 1000000

	lastID := ss.lastID()

	next := fmt.Sprintf("%d-%d", ts, 0)
	if streamCmp(lastID, next) == -1 {
		return next
	}
	last := parseStreamID(lastID)
	return fmt.Sprintf("%d-%d", last[0], last[1]+1)
}

func (ss *streamKey) lastID() string {
	if len(*ss) == 0 {
		return "0-0"
	}

	return (*ss)[len(*ss)-1].ID
}

func parseStreamID(id string) [2]uint64 {
	var res [2]uint64
	parts := strings.SplitN(id, "-", 2)
	res[0], _ = strconv.ParseUint(parts[0], 10, 64)
	if len(parts) == 2 {
		res[1], _ = strconv.ParseUint(parts[1], 10, 64)
	}
	return res
}

// compares two stream IDs (of the full format: "123-123"). Returns: -1, 0, 1
func streamCmp(a, b string) int {
	ap := parseStreamID(a)
	bp := parseStreamID(b)
	if ap[0] < bp[0] {
		return -1
	}
	if ap[0] > bp[0] {
		return 1
	}
	if ap[1] < bp[1] {
		return -1
	}
	if ap[1] > bp[1] {
		return 1
	}
	return 0
}

// formatStreamID makes a full id ("42-42") out of a partial one ("42")
func formatStreamID(id string) (string, error) {
	var ts [2]uint64
	parts := strings.SplitN(id, "-", 2)

	if len(parts) > 0 {
		p, err := strconv.ParseUint(parts[0], 10, 64)
		if err != nil {
			return "", errInvalidEntryID
		}
		ts[0] = p
	}
	if len(parts) > 1 {
		p, err := strconv.ParseUint(parts[1], 10, 64)
		if err != nil {
			return "", errInvalidEntryID
		}
		ts[1] = p
	}
	return fmt.Sprintf("%d-%d", ts[0], ts[1]), nil
}

func formatStreamRangeBound(id string, start bool, reverse bool) (string, error) {
	if id == "-" {
		return "0-0", nil
	}

	if id == "+" {
		return fmt.Sprintf("%d-%d", uint64(math.MaxUint64), uint64(math.MaxUint64)), nil
	}

	if id == "0" {
		return "0-0", nil
	}

	parts := strings.Split(id, "-")
	if len(parts) == 2 {
		return formatStreamID(id)
	}

	// Incomplete IDs case
	ts, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return "", errInvalidEntryID
	}

	if (!start && !reverse) || (start && reverse) {
		return fmt.Sprintf("%d-%d", ts, uint64(math.MaxUint64)), nil
	}

	return fmt.Sprintf("%d-%d", ts, 0), nil
}

func reversedStreamEntries(o []StreamEntry) []StreamEntry {
	newStream := make([]StreamEntry, len(o))

	for i, e := range o {
		newStream[len(o)-i-1] = e
	}

	return newStream
}

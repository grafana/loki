package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log/level"
)

// parseMismatchLogs converts raw logfmt lines into structured MismatchEntry values.
// Lines that cannot be parsed are logged and skipped.
func parseMismatchLogs(lines []string) []*MismatchEntry {
	var entries []*MismatchEntry
	for i, line := range lines {
		entry, err := parseSingleLog(line)
		if err != nil {
			level.Warn(logger).Log("msg", "skipping unparseable log line", "line_index", i, "err", err)
			continue
		}
		entries = append(entries, entry)
	}
	return entries
}

// parseSingleLog parses one logfmt-formatted line into a MismatchEntry.
func parseSingleLog(line string) (*MismatchEntry, error) {
	fields := parseLogfmt(line)

	if fields["comparison_status"] != "mismatch" {
		return nil, fmt.Errorf("not a mismatch line")
	}

	entry := &MismatchEntry{
		CorrelationID:        fields["correlation_id"],
		Tenant:               fields["tenant"],
		Query:                fields["query"],
		QueryType:            fields["query_type"],
		MismatchCause:        fields["mismatch_cause"],
		CellAResultURI:       fields["cell_a_result_uri"],
		CellBResultURI:       fields["cell_b_result_uri"],
		CellAEntriesReturned: parseInt64(fields["cell_a_entries_returned"]),
		CellBEntriesReturned: parseInt64(fields["cell_b_entries_returned"]),
		CellABytesProcessed:  parseInt64(fields["cell_a_bytes_processed"]),
		CellBBytesProcessed:  parseInt64(fields["cell_b_bytes_processed"]),
		CellALinesProcessed:  parseInt64(fields["cell_a_lines_processed"]),
		CellBLinesProcessed:  parseInt64(fields["cell_b_lines_processed"]),
		CellAExecTimeMs:      parseInt64(fields["cell_a_exec_time_ms"]),
		CellBExecTimeMs:      parseInt64(fields["cell_b_exec_time_ms"]),
		CellAStatusCode:      int(parseInt64(fields["cell_a_status_code"])),
		CellBStatusCode:      int(parseInt64(fields["cell_b_status_code"])),
		CellAUsedNewEngine:   fields["cell_a_used_new_engine"] == "true",
		CellBUsedNewEngine:   fields["cell_b_used_new_engine"] == "true",
	}

	if v := fields["start_time"]; v != "" {
		entry.StartTime = timeFromUnixNano(v)
	}
	if v := fields["end_time"]; v != "" {
		entry.EndTime = timeFromUnixNano(v)
	}

	if entry.CorrelationID == "" {
		return nil, fmt.Errorf("missing correlation_id")
	}

	return entry, nil
}

// parseLogfmt does a simple key=value parse of a logfmt line.
// Handles quoted values and bare values.
func parseLogfmt(line string) map[string]string {
	fields := make(map[string]string)
	remaining := line

	for len(remaining) > 0 {
		remaining = strings.TrimLeft(remaining, " \t")
		if len(remaining) == 0 {
			break
		}

		eqIdx := strings.IndexByte(remaining, '=')
		if eqIdx < 0 {
			break
		}

		key := remaining[:eqIdx]
		remaining = remaining[eqIdx+1:]

		var value string
		if len(remaining) > 0 && remaining[0] == '"' {
			// Quoted value: find the closing quote, handling escaped quotes.
			end := 1
			for end < len(remaining) {
				if remaining[end] == '\\' && end+1 < len(remaining) {
					end += 2
					continue
				}
				if remaining[end] == '"' {
					break
				}
				end++
			}
			if end < len(remaining) {
				value = remaining[1:end]
				remaining = remaining[end+1:]
			} else {
				value = remaining[1:]
				remaining = ""
			}
			value = strings.ReplaceAll(value, `\"`, `"`)
		} else {
			// Bare value: read until next space.
			spIdx := strings.IndexAny(remaining, " \t")
			if spIdx < 0 {
				value = remaining
				remaining = ""
			} else {
				value = remaining[:spIdx]
				remaining = remaining[spIdx:]
			}
		}

		fields[key] = value
	}

	return fields
}

func parseInt64(s string) int64 {
	if s == "" {
		return 0
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		level.Warn(logger).Log("msg", "failed to parse int64 from log field", "value", s, "err", err)
		return 0
	}
	return v
}

func timeFromUnixNano(s string) time.Time {
	ns := parseInt64(s)
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns).UTC()
}

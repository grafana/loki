// SPDX-License-Identifier: BSD-3-Clause
//go:build aix && !cgo

package cpu

import (
	"context"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v4/internal/common"
)

func TimesWithContext(ctx context.Context, percpu bool) ([]TimesStat, error) {
	if percpu {
		return []TimesStat{}, common.ErrNotImplementedError
	} else {
		out, err := invoke.CommandWithContext(ctx, "sar", "-u", "10", "1")
		if err != nil {
			return nil, err
		}
		lines := strings.Split(string(out), "\n")
		if len(lines) < 5 {
			return []TimesStat{}, common.ErrNotImplementedError
		}

		ret := TimesStat{CPU: "cpu-total"}
		h := strings.Fields(lines[len(lines)-3]) // headers
		v := strings.Fields(lines[len(lines)-2]) // values
		for i, header := range h {
			if t, err := strconv.ParseFloat(v[i], 64); err == nil {
				switch header {
				case `%usr`:
					ret.User = t
				case `%sys`:
					ret.System = t
				case `%wio`:
					ret.Iowait = t
				case `%idle`:
					ret.Idle = t
				}
			}
		}

		return []TimesStat{ret}, nil
	}
}

func InfoWithContext(ctx context.Context) ([]InfoStat, error) {
	out, err := invoke.CommandWithContext(ctx, "prtconf")
	if err != nil {
		return nil, err
	}

	ret := InfoStat{}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Number Of Processors:") {
			p := strings.Fields(line)
			if len(p) > 3 {
				if t, err := strconv.ParseUint(p[3], 10, 64); err == nil {
					ret.Cores = int32(t)
				}
			}
		} else if strings.HasPrefix(line, "Processor Clock Speed:") {
			p := strings.Fields(line)
			if len(p) > 4 {
				if t, err := strconv.ParseFloat(p[3], 64); err == nil {
					switch strings.ToUpper(p[4]) {
					case "MHZ":
						ret.Mhz = t
					case "GHZ":
						ret.Mhz = t * 1000.0
					case "KHZ":
						ret.Mhz = t / 1000.0
					default:
						ret.Mhz = t
					}
				}
			}
			break
		}
	}
	return []InfoStat{ret}, nil
}

func CountsWithContext(ctx context.Context, logical bool) (int, error) {
	info, err := InfoWithContext(ctx)
	if err == nil {
		return int(info[0].Cores), nil
	}
	return 0, err
}

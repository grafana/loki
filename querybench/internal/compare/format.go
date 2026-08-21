package compare

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// formatDuration renders a latency in seconds as milliseconds below one second
// and as seconds above it, so both fast and slow queries stay readable.
func formatDuration(seconds float64) string {
	if math.IsNaN(seconds) {
		return "–"
	}
	if seconds < 1 {
		return fmt.Sprintf("%.0f ms", seconds*1000)
	}
	return fmt.Sprintf("%.2f s", seconds)
}

// formatBytes renders a byte count with a decimal (base-1000) unit, matching how
// Loki reports processed bytes.
func formatBytes(v float64) string {
	return humanize(v, 1000, []string{"B", "KB", "MB", "GB", "TB", "PB"})
}

// formatBytesPerSecond renders a byte rate.
func formatBytesPerSecond(v float64) string {
	return formatBytes(v) + "/s"
}

// formatCount renders a dimensionless count with a K/M/B suffix above a
// thousand.
func formatCount(v float64) string {
	return humanize(v, 1000, []string{"", "K", "M", "B", "T"})
}

// formatCores renders a CPU core count.
func formatCores(v float64) string {
	return fmt.Sprintf("%.2f", v)
}

// humanize scales v down by base until it fits a unit, returning at most two
// decimals. Negative values keep their sign.
func humanize(v, base float64, units []string) string {
	if v == 0 {
		return "0 " + strings.TrimSpace(units[0])
	}
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	i := 0
	for v >= base && i < len(units)-1 {
		v /= base
		i++
	}
	unit := units[i]
	if unit == "" {
		return sign + trimZeros(fmt.Sprintf("%.2f", v))
	}
	return sign + trimZeros(fmt.Sprintf("%.2f", v)) + " " + unit
}

// trimZeros removes trailing zeros and a trailing decimal point so "1.20"
// becomes "1.2" and "3.00" becomes "3".
func trimZeros(s string) string {
	if !strings.Contains(s, ".") {
		return s
	}
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

// shortDuration renders a duration compactly as whole hours, minutes and
// seconds, dropping any zero component (e.g. 6h, 15m, 30s, 24h15m). Sub-second
// durations, which the tool does not produce for windows or steps, fall back to
// the standard form.
func shortDuration(d time.Duration) string {
	if d == 0 {
		return "0s"
	}
	if d%time.Second != 0 {
		return d.String()
	}

	var b strings.Builder
	if h := d / time.Hour; h > 0 {
		fmt.Fprintf(&b, "%dh", h)
	}
	if m := (d % time.Hour) / time.Minute; m > 0 {
		fmt.Fprintf(&b, "%dm", m)
	}
	if s := (d % time.Minute) / time.Second; s > 0 {
		fmt.Fprintf(&b, "%ds", s)
	}
	return b.String()
}

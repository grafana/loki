// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in https://raw.githubusercontent.com/golang/go/master/LICENSE

package str2duration

import (
	"errors"
	"time"
)

var unitMap = map[string]int64{
	"ns": int64(time.Nanosecond),
	"us": int64(time.Microsecond),
	"µs": int64(time.Microsecond), // U+00B5 = micro symbol
	"μs": int64(time.Microsecond), // U+03BC = Greek letter mu
	"ms": int64(time.Millisecond),
	"s":  int64(time.Second),
	"m":  int64(time.Minute),
	"h":  int64(time.Hour),
	"d":  int64(time.Hour) * 24,
	"w":  int64(time.Hour) * 168,
}

// ParseDuration parses a duration string.
// A duration string is a possibly signed sequence of
// decimal numbers, each with optional fraction and a unit suffix,
// such as "300ms", "-1.5h" or "2h45m".
// Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h", "d", "w".
func ParseDuration(s string) (time.Duration, error) {
	// [-+]?([0-9]*(\.[0-9]*)?[a-z]+)+
	orig := s
	var d int64
	neg := false

	// Consume [-+]?
	if s != "" {
		c := s[0]
		if c == '-' || c == '+' {
			neg = c == '-'
			s = s[1:]
		}
	}
	// Special case: if all that is left is "0", this is zero.
	if s == "0" {
		return 0, nil
	}
	if s == "" {
		return 0, errors.New("time: invalid duration " + quote(orig))
	}
	for s != "" {
		var (
			v, f  int64       // integers before, after decimal point
			scale float64 = 1 // value = v + f/scale
		)

		var err error

		// The next character must be [0-9.]
		if !(s[0] == '.' || '0' <= s[0] && s[0] <= '9') {
			return 0, errors.New("time: invalid duration " + quote(orig))
		}
		// Consume [0-9]*
		pl := len(s)
		v, s, err = leadingInt(s)
		if err != nil {
			return 0, errors.New("time: invalid duration " + quote(orig))
		}
		pre := pl != len(s) // whether we consumed anything before a period

		// Consume (\.[0-9]*)?
		post := false
		if s != "" && s[0] == '.' {
			s = s[1:]
			pl := len(s)
			f, scale, s = leadingFraction(s)
			post = pl != len(s)
		}
		if !pre && !post {
			// no digits (e.g. ".s" or "-.s")
			return 0, errors.New("time: invalid duration " + quote(orig))
		}

		// Consume unit.
		i := 0
		for ; i < len(s); i++ {
			c := s[i]
			if c == '.' || '0' <= c && c <= '9' {
				break
			}
		}
		if i == 0 {
			return 0, errors.New("time: missing unit in duration " + quote(orig))
		}
		u := s[:i]
		s = s[i:]
		unit, ok := unitMap[u]
		if !ok {
			return 0, errors.New("time: unknown unit " + quote(u) + " in duration " + quote(orig))
		}
		if v > (1<<63-1)/unit {
			// overflow
			return 0, errors.New("time: invalid duration " + quote(orig))
		}
		v *= unit
		if f > 0 {
			// float64 is needed to be nanosecond accurate for fractions of hours.
			// v >= 0 && (f*unit/scale) <= 3.6e+12 (ns/h, h is the largest unit)
			v += int64(float64(f) * (float64(unit) / scale))
			if v < 0 {
				// overflow
				return 0, errors.New("time: invalid duration " + quote(orig))
			}
		}
		d += v
		if d < 0 {
			// overflow
			return 0, errors.New("time: invalid duration " + quote(orig))
		}
	}

	if neg {
		d = -d
	}
	return time.Duration(d), nil
}

func quote(s string) string {
	return "\"" + s + "\""
}

var errLeadingInt = errors.New("time: bad [0-9]*") // never printed

// leadingInt consumes the leading [0-9]* from s.
func leadingInt(s string) (x int64, rem string, err error) {
	i := 0
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		if x > (1<<63-1)/10 {
			// overflow
			return 0, "", errLeadingInt
		}
		x = x*10 + int64(c) - '0'
		if x < 0 {
			// overflow
			return 0, "", errLeadingInt
		}
	}
	return x, s[i:], nil
}

// leadingFraction consumes the leading [0-9]* from s.
// It is used only for fractions, so does not return an error on overflow,
// it just stops accumulating precision.
func leadingFraction(s string) (x int64, scale float64, rem string) {
	i := 0
	scale = 1
	overflow := false
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			break
		}
		if overflow {
			continue
		}
		if x > (1<<63-1)/10 {
			// It's possible for overflow to give a positive number, so take care.
			overflow = true
			continue
		}
		y := x*10 + int64(c) - '0'
		if y < 0 {
			overflow = true
			continue
		}
		x = y
		scale *= 10
	}
	return x, scale, s[i:]
}

// String returns a string representing the duration in the form "1w4d2h3m5s".
// Units with 0 values aren't returned, for example: 1d1ms is 1 day 1 milliseconds
func String(d time.Duration) string {
	if d == 0 {
		return "0s"
	}

	// Largest time is 15250w1d23h47m16s854ms775us807ns
	var buf [32]byte
	w := len(buf)
	var sign string

	u := uint64(d)
	neg := d < 0
	if neg {
		u = -u
		sign = "-"
	}

	// u is nanoseconds (ns)
	if u > 0 {
		w--

		if u%1000 > 0 {
			buf[w] = 's'
			w--
			buf[w] = 'n'
			w = fmtInt(buf[:w], u%1000)
		} else {
			w++
		}

		u /= 1000

		// u is now integer microseconds (us)
		if u > 0 {
			w--
			if u%1000 > 0 {
				buf[w] = 's'
				w--
				buf[w] = 'u'
				w = fmtInt(buf[:w], u%1000)
			} else {
				w++
			}
			u /= 1000

			// u is now integer milliseconds (ms)
			if u > 0 {
				w--
				if u%1000 > 0 {
					buf[w] = 's'
					w--
					buf[w] = 'm'
					w = fmtInt(buf[:w], u%1000)
				} else {
					w++
				}
				u /= 1000

				// u is now integer seconds (s)
				if u > 0 {
					w--
					if u%60 > 0 {
						buf[w] = 's'
						w = fmtInt(buf[:w], u%60)
					} else {
						w++
					}
					u /= 60

					// u is now integer minutes (m)
					if u > 0 {
						w--

						if u%60 > 0 {
							buf[w] = 'm'
							w = fmtInt(buf[:w], u%60)
						} else {
							w++
						}

						u /= 60

						// u is now integer hours (h)
						if u > 0 {
							w--

							if u%24 > 0 {
								buf[w] = 'h'
								w = fmtInt(buf[:w], u%24)
							} else {
								w++
							}

							u /= 24

							// u is now integer days (d)
							if u > 0 {
								w--

								if u%7 > 0 {
									buf[w] = 'd'
									w = fmtInt(buf[:w], u%7)
								} else {
									w++
								}

								u /= 7

								// u is now integer weeks (w)
								if u > 0 {
									w--
									buf[w] = 'w'
									w = fmtInt(buf[:w], u)
								}

							}

						}
					}
				}
			}
		}

	}

	return sign + string(buf[w:])
}

// fmtInt formats v into the tail of buf.
// It returns the index where the output begins.
func fmtInt(buf []byte, v uint64) int {
	w := len(buf)
	if v == 0 {
		w--
		buf[w] = '0'
	} else {
		for v > 0 {
			w--
			buf[w] = byte(v%10) + '0'
			v /= 10
		}
	}
	return w
}

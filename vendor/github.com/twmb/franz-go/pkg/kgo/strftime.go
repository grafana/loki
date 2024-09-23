package kgo

import (
	"strconv"
	"time"
)

// NOTE: this code is copied from github.com/twmb/go-strftime, with AppendFormat
// being unexported.

// appendFormat appends t to dst according to the input strftime format.
//
// this does not take into account locale; some high level differences:
//
//	%E and %O are stripped, as well as a single subsequent alpha char
//	%x is DD/MM/YY
//	%c is time.ANSIC
//
// In normal strftime, %a, %A, %b, %B, %c, %p, %P, %r, %x, and %X are all
// affected by locale. This package hardcodes the implementation to mirror
// LC_TIME=C (minus %x). Every strftime(3) formatter is accounted for.
func strftimeAppendFormat(dst []byte, format string, t time.Time) []byte {
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c != '%' || i == len(format)-1 {
			dst = append(dst, c)
			continue
		}

		i++
		c = format[i]
		switch c {
		default:
			dst = append(dst, '%', c)
		case 'a': // abbrev day
			dst = t.AppendFormat(dst, "Mon")
		case 'A': // full day
			dst = t.AppendFormat(dst, "Monday")
		case 'b', 'h': // abbrev month, h is equivalent to b
			dst = t.AppendFormat(dst, "Jan")
		case 'B': // full month
			dst = t.AppendFormat(dst, "January")
		case 'c': // preferred date and time representation
			dst = t.AppendFormat(dst, time.ANSIC)
		case 'C': // century (year/100) as two digit num
			dst = append0Pad(dst, t.Year()/100, 2)
		case 'd': // day of month as two digit num
			dst = append0Pad(dst, t.Day(), 2)
		case 'D': // %m/%d/%y
			dst = append0Pad(dst, int(t.Month()), 2)
			dst = append(dst, '/')
			dst = append0Pad(dst, t.Day(), 2)
			dst = append(dst, '/')
			dst = append0Pad(dst, t.Year()%100, 2)
		case 'e': // day of month as num like %d, but leading 0 is space instead
			dst = appendSpacePad(dst, t.Day())
		case 'E', 'O': // modifier, ignored and skip next (if ascii)
			if i+1 < len(format) {
				next := format[i+1]
				if 'a' <= next && next <= 'z' || 'A' <= next && next <= 'Z' {
					i++
				}
			}
		case 'F': // %Y-%m-%d (iso8601)
			dst = strconv.AppendInt(dst, int64(t.Year()), 10)
			dst = append(dst, '-')
			dst = append0Pad(dst, int(t.Month()), 2)
			dst = append(dst, '-')
			dst = append0Pad(dst, t.Day(), 2)
		case 'G': // iso8601 week-based year
			year, _ := t.ISOWeek()
			dst = append0Pad(dst, year, 4)
		case 'g': // like %G, but two digit year (no century)
			year, _ := t.ISOWeek()
			dst = append0Pad(dst, year%100, 2)
		case 'H': // hour as number on 24hr clock
			dst = append0Pad(dst, t.Hour(), 2)
		case 'I': // hour as number on 12hr clock
			dst = append0Pad(dst, t.Hour()%12, 2)
		case 'j': // day of year as decimal number
			dst = append0Pad(dst, t.YearDay(), 3)
		case 'k': // 24hr as number, space padded
			dst = appendSpacePad(dst, t.Hour())
		case 'l': // 12hr as number, space padded
			dst = appendSpacePad(dst, t.Hour()%12)
		case 'm': // month as number
			dst = append0Pad(dst, int(t.Month()), 2)
		case 'M': // minute as number
			dst = append0Pad(dst, t.Minute(), 2)
		case 'n': // newline
			dst = append(dst, '\n')
		case 'p': // AM or PM
			dst = appendAMPM(dst, t.Hour())
		case 'P': // like %p buf lowercase
			dst = appendampm(dst, t.Hour())
		case 'r': // %I:%M:%S %p
			h := t.Hour()
			dst = append0Pad(dst, h%12, 2)
			dst = append(dst, ':')
			dst = append0Pad(dst, t.Minute(), 2)
			dst = append(dst, ':')
			dst = append0Pad(dst, t.Second(), 2)
			dst = append(dst, ' ')
			dst = appendAMPM(dst, h)
		case 'R': // %H:%M
			dst = append0Pad(dst, t.Hour(), 2)
			dst = append(dst, ':')
			dst = append0Pad(dst, t.Minute(), 2)
		case 's': // seconds since epoch
			dst = strconv.AppendInt(dst, t.Unix(), 10)
		case 'S': // second as number thru 60 for leap second
			dst = append0Pad(dst, t.Second(), 2)
		case 't': // tab
			dst = append(dst, '\t')
		case 'T': // %H:%M:%S
			dst = append0Pad(dst, t.Hour(), 2)
			dst = append(dst, ':')
			dst = append0Pad(dst, t.Minute(), 2)
			dst = append(dst, ':')
			dst = append0Pad(dst, t.Second(), 2)
		case 'u': // day of week as num; Monday is 1
			day := byte(t.Weekday())
			if day == 0 {
				day = 7
			}
			dst = append(dst, '0'+day)
		case 'U': // week number of year starting from first Sunday
			dst = append0Pad(dst, (t.YearDay()-int(t.Weekday())+7)/7, 2)
		case 'V': // iso8601 week number
			_, week := t.ISOWeek()
			dst = append0Pad(dst, week, 2)
		case 'w': // day of week, 0 to 6, Sunday 0
			dst = strconv.AppendInt(dst, int64(t.Weekday()), 10)
		case 'W': // week number of year starting from first Monday
			dst = append0Pad(dst, (t.YearDay()-(int(t.Weekday())+6)%7+7)/7, 2)
		case 'x': // date representation for current locale; we go DD/MM/YY
			dst = append0Pad(dst, t.Day(), 2)
			dst = append(dst, '/')
			dst = append0Pad(dst, int(t.Month()), 2)
			dst = append(dst, '/')
			dst = append0Pad(dst, t.Year()%100, 2)
		case 'X': // time representation for current locale; we go HH:MM:SS
			dst = append0Pad(dst, t.Hour(), 2)
			dst = append(dst, ':')
			dst = append0Pad(dst, t.Minute(), 2)
			dst = append(dst, ':')
			dst = append0Pad(dst, t.Second(), 2)
		case 'y': // year as num without century
			dst = append0Pad(dst, t.Year()%100, 2)
		case 'Y': // year as a num
			dst = append0Pad(dst, t.Year(), 4)
		case 'z': // +hhmm or -hhmm offset from utc
			dst = t.AppendFormat(dst, "-0700")
		case 'Z': // timezone
			dst = t.AppendFormat(dst, "MST")
		case '+': // date and time in date(1) format
			dst = t.AppendFormat(dst, "Mon Jan _2 15:04:05 MST 2006")
		case '%':
			dst = append(dst, '%')
		}
	}
	return dst
}

// all space padded numbers are two length
func appendSpacePad(p []byte, n int) []byte {
	if n < 10 {
		return append(p, ' ', '0'+byte(n))
	}
	return strconv.AppendInt(p, int64(n), 10)
}

func append0Pad(dst []byte, n, size int) []byte {
	switch size {
	case 4:
		if n < 1000 {
			dst = append(dst, '0')
		}
		fallthrough
	case 3:
		if n < 100 {
			dst = append(dst, '0')
		}
		fallthrough
	case 2:
		if n < 10 {
			dst = append(dst, '0')
		}
	}
	return strconv.AppendInt(dst, int64(n), 10)
}

func appendampm(p []byte, h int) []byte {
	if h < 12 {
		return append(p, 'a', 'm')
	}
	return append(p, 'p', 'm')
}

func appendAMPM(p []byte, h int) []byte {
	if h < 12 {
		return append(p, 'A', 'M')
	}
	return append(p, 'P', 'M')
}

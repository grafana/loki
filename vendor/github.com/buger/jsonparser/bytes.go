// SYS-REQ-015, SYS-REQ-058, SYS-REQ-059, SYS-REQ-064: integer parsing internals
package jsonparser

const absMinInt64 = 1 << 63
const maxInt64 = 1<<63 - 1
const maxUint64 = 1<<64 - 1

// About 2x faster then strconv.ParseInt because it only supports base 10, which is enough for JSON
func parseInt(bytes []byte) (v int64, ok bool, overflow bool) {
	l := len(bytes)
	if l == 0 {
		return 0, false, false
	}

	var neg bool = false
	i := 0
	if bytes[0] == '-' {
		neg = true
		i = 1
	}
	if i == l {
		return 0, false, false
	}

	if l-i < 19 {
		for ; i < l; i++ {
			d := bytes[i] - '0'
			if d > 9 {
				return 0, false, false
			}
			v = 10*v + int64(d)
		}

		if neg {
			return -v, true, false
		}
		return v, true, false
	}

	if neg {
		bytes = bytes[1:]
	}

	var n uint64 = 0
	for _, c := range bytes {
		if c < '0' || c > '9' {
			return 0, false, false
		}
		if n > maxUint64/10 {
			return 0, false, true
		}
		n *= 10
		n1 := n + uint64(c-'0')
		if n1 < n {
			return 0, false, true
		}
		n = n1
	}

	if n > maxInt64 {
		if neg && n == absMinInt64 {
			return -absMinInt64, true, false
		}
		return 0, false, true
	}

	if neg {
		return -int64(n), true, false
	} else {
		return int64(n), true, false
	}
}

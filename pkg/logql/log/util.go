package log

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

func uniqueString(s []string) []string {
	unique := make(map[string]bool, len(s))
	us := make([]string, len(unique))
	for _, elem := range s {
		if len(elem) != 0 {
			if !unique[elem] {
				us = append(us, elem)
				unique[elem] = true
			}
		}
	}
	return us
}

func sanitizeLabelKey(key string, isPrefix bool) string {
	if len(key) == 0 {
		return key
	}
	key = strings.TrimSpace(key)
	if len(key) == 0 {
		return key
	}
	if isPrefix && key[0] >= '0' && key[0] <= '9' {
		key = "_" + key
	}
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (r >= '0' && r <= '9') {
			return r
		}
		return '_'
	}, key)
}

func sanitizeLabelTo(key, buf []byte, isPrefix bool) []byte {
	if len(key) == 0 {
		return buf[:0]
	}
	key = bytes.TrimSpace(key)
	if len(key) == 0 {
		return buf[:0]
	}
	res := buf[:0]
	if isPrefix && key[0] >= '0' && key[0] <= '9' {
		res = append(res, '_')
	}
	for i := 0; i < len(key); {
		wid := 1
		r := rune(key[i])
		if r >= utf8.RuneSelf {
			_, wid = utf8.DecodeRune(key[i:])
			res = append(res, '_')
			i += wid
			continue
		}
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (r >= '0' && r <= '9') {
			res = append(res, key[i])
		} else {
			res = append(res, '_')
		}
		i += wid
	}
	return res
}

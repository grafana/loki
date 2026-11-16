package logfmt

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type scannedPair struct {
	ok    bool
	key   string
	value string
}

func TestDecoder_ScanKeyval(t *testing.T) {
	for _, tc := range []struct {
		name    string
		line    string
		want    []scannedPair
		wantErr string
	}{
		{
			name: "unquoted values return each pair",
			line: `a=1 b=two`,
			want: []scannedPair{{true, "a", "1"}, {true, "b", "two"}},
		},
		{
			name: "a quoted value keeps its spaces",
			line: `msg="hello world" x=1`,
			want: []scannedPair{{true, "msg", "hello world"}, {true, "x", "1"}},
		},
		{
			name: "an escaped quote in a quoted value is unquoted",
			line: `msg="say \"hi\"" x=1`,
			want: []scannedPair{{true, "msg", `say "hi"`}, {true, "x", "1"}},
		},
		{
			name: "a quoted value that starts with an escaped quote is unquoted",
			line: `msg="\"hi\" there" x=1`,
			want: []scannedPair{{true, "msg", `"hi" there`}, {true, "x", "1"}},
		},
		{
			name: "an escaped backslash before the closing quote ends the value",
			line: `msg="a\\" x=1`,
			want: []scannedPair{{true, "msg", `a\`}, {true, "x", "1"}},
		},
		{
			name: "an escaped backslash and an escaped quote are both unquoted",
			line: `msg="a\\\"b" x=1`,
			want: []scannedPair{{true, "msg", `a\"b`}, {true, "x", "1"}},
		},
		{
			name: "an empty quoted value returns an empty value",
			line: `a="" b=1`,
			want: []scannedPair{{true, "a", ""}, {true, "b", "1"}},
		},
		{
			name: "a quoted value followed by a key without a space returns both pairs",
			line: `a="x"b=2`,
			want: []scannedPair{{true, "a", "x"}, {true, "b", "2"}},
		},
		{
			name: "multibyte characters in a quoted value are kept",
			line: `msg="é ü" k=v`,
			want: []scannedPair{{true, "msg", "é ü"}, {true, "k", "v"}},
		},
		{
			name: "a tab, a newline and a carriage return separate pairs",
			line: "a=1\tb=2\nc=3\rd=4",
			want: []scannedPair{{true, "a", "1"}, {true, "b", "2"}, {true, "c", "3"}, {true, "d", "4"}},
		},
		{
			name: "a key without an equals sign returns an empty value",
			line: `a b=2`,
			want: []scannedPair{{true, "a", ""}, {true, "b", "2"}},
		},
		{
			name: "an equals sign without a value returns an empty value",
			line: `a= b=2`,
			want: []scannedPair{{true, "a", ""}, {true, "b", "2"}},
		},
		{
			name:    "an unterminated quoted value fails",
			line:    `a="abc`,
			want:    []scannedPair{{false, "a", ""}},
			wantErr: "logfmt syntax error at pos 7 : unterminated quoted value",
		},
		{
			name:    "an escaped closing quote leaves the value unterminated",
			line:    `a="abc\"`,
			want:    []scannedPair{{false, "a", ""}},
			wantErr: "logfmt syntax error at pos 9 : unterminated quoted value",
		},
		{
			name:    "a quote in a key fails and the next pair is read",
			line:    `"a"=1 b=2`,
			want:    []scannedPair{{false, "", ""}, {true, "b", "2"}},
			wantErr: `logfmt syntax error at pos 1 : unexpected '"'`,
		},
		{
			name:    "an equals sign in a value fails and the next pair is read",
			line:    `a=b=c d=1`,
			want:    []scannedPair{{false, "a", ""}, {true, "d", "1"}},
			wantErr: "logfmt syntax error at pos 4 : unexpected '='",
		},
		{
			name:    "a quote in an unquoted value fails and the next pair is read",
			line:    `a=b"c d=1`,
			want:    []scannedPair{{false, "a", ""}, {true, "d", "1"}},
			wantErr: `logfmt syntax error at pos 4 : unexpected '"'`,
		},
		{
			name:    "a leading equals sign fails and the next pair is read",
			line:    `=x a=1`,
			want:    []scannedPair{{false, "", ""}, {true, "a", "1"}},
			wantErr: "logfmt syntax error at pos 1 : unexpected '='",
		},
		{
			name:    "an invalid UTF-8 key fails and the next pair is read",
			line:    "a\xffb=1 c=2",
			want:    []scannedPair{{false, "a\xffb", ""}, {true, "c", "2"}},
			wantErr: "logfmt syntax error at pos 4 : invalid key",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []scannedPair
			dec := NewDecoder([]byte(tc.line))
			for !dec.EOL() {
				ok := dec.ScanKeyval()
				got = append(got, scannedPair{ok, string(dec.Key()), string(dec.Value())})
			}

			var gotErr string
			if dec.Err() != nil {
				gotErr = dec.Err().Error()
			}
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.wantErr, gotErr)
		})
	}
}

func BenchmarkDecoder_ScanKeyval(b *testing.B) {
	lines := make([][]byte, 256)
	for i := range lines {
		lines[i] = []byte(fmt.Sprintf(
			`level=error ts=2026-10-26T20:53:%02d.%09dZ caller=handler.go:%d component=worker msg="failed to process request" err="context deadline exceeded" duration=%d.%03ds method=POST path=/api/v1/items/%d status=500 user=u-%d trace_id=%032x retries=%d region=us-east-0`,
			i%60, i*977, 100+i%400, i%30, i%1000, i, i%5000, uint64(i)*2654435761, i%4))
	}
	dec := NewDecoder(nil)

	b.SetBytes(int64(len(lines[0])))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		dec.Reset(lines[i%len(lines)])
		for !dec.EOL() {
			dec.ScanKeyval()
		}
	}
}

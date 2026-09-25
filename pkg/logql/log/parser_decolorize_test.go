package log

import (
	"bytes"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
)

func TestStripANSI(t *testing.T) {
	cases := []struct {
		in   []byte
		want []byte
	}{
		{[]byte("\x1b[32mhello\x1b[0m"), []byte("hello")},
		{[]byte("\x1b[1;31merr\x1b[0m=bad"), []byte("err=bad")},
		{[]byte("clean"), []byte("clean")},
		{[]byte{}, []byte{}},
	}

	for i, tc := range cases {
		got := StripANSI(tc.in)
		if bytes.Contains(got, []byte{0x1b}) {
			t.Fatalf("case %d: expected no ESC in %q", i, got)
		}
		if !bytes.Equal(got, tc.want) {
			t.Fatalf("case %d: got %q, want %q", i, got, tc.want)
		}
	}
}

func makeLBS() *LabelsBuilder {
	b := NewBaseLabelsBuilder()
	baseLabels := labels.Labels{} // or use the actual type for base labels; empty set rather than nil
	return b.ForLabels(baseLabels, 0)
}

func TestLogfmtProcess_StripsANSIFromReturnedLine(t *testing.T) {
	p := NewLogfmtParser(false, true)
	lbs := makeLBS()

	colored := []byte("\x1b[32mlevel\x1b[0m=info msg=\"ok\"")
	gotLine, ok := p.Process(0, colored, lbs)
	if !ok {
		t.Fatalf("Process returned ok=false for colored logfmt line")
	}
	if bytes.Contains(gotLine, []byte{0x1b}) {
		t.Fatalf("returned line still contained ANSI escapes: %q", gotLine)
	}
	if !bytes.Contains(gotLine, []byte("level=info")) {
		t.Fatalf("returned line lost expected content, got: %q", gotLine)
	}
}

func TestLogfmtProcess_ParsesColoredKeyAndValue(t *testing.T) {
	p := NewLogfmtParser(false, true)
	lbs := makeLBS()

	line := []byte("\x1b[32mlevel\x1b[0m=info \x1b[36mstatus\x1b[0m=200 msg=\"\x1b[31mhot\x1b[0m\"")

	_, ok := p.Process(0, line, lbs)
	if !ok {
		t.Fatalf("Process returned ok=false")
	}

	m, _ := lbs.Map()

	if got := m["level"]; string(got) != "info" {
		t.Fatalf("expected level=info, got %q", got)
	}
	if got := m["status"]; string(got) != "200" {
		t.Fatalf("expected status=200, got %q", got)
	}
	if got := m["msg"]; string(got) != "hot" {
		t.Fatalf("expected msg=hot, got %q", got)
	}
}

func TestLogfmtProcess_NoANSI_Idempotent(t *testing.T) {
	p := NewLogfmtParser(true, true)
	lbs := makeLBS()

	in := []byte("level=debug a=1 b=\"two words\"")
	out, ok := p.Process(0, in, lbs)
	if !ok {
		t.Fatalf("Process returned ok=false for clean line")
	}
	if !bytes.Equal(in, out) {
		t.Fatalf("expected returned line to equal input for clean lines.\n in=%q\nout=%q", in, out)
	}

	m, _ := lbs.Map()
	if string(m["level"]) != "debug" || string(m["a"]) != "1" || string(m["b"]) != "two words" {
		t.Fatalf("unexpected parsed labels: %#v", m)
	}
}

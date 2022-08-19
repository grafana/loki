package starlark

import (
	"bytes"
	"fmt"
	"strings"
)

type Starlark struct {
	buf       *bytes.Buffer
	isNewline bool
	index     int
}

func (s *Starlark) Write(msg string) {
	if s.isNewline {
		s.buf.WriteString(strings.Repeat(" ", s.index))
	}
	s.buf.WriteString(msg)
	last := msg[len(msg)-1:]
	s.isNewline = last == "\n"
}

func (s *Starlark) Indent(change int) {
	s.index += change
}

func (s *Starlark) methodName(name, suffix string) string {
	r := strings.NewReplacer("-", "_", " ", "_", ":", "")
	name = r.Replace(name)
	if suffix != "" && !strings.HasSuffix(name, "_"+suffix) {
		name += "_" + suffix
	}
	return name
}

func (s *Starlark) MethodStart(name string) {
	s.Write(fmt.Sprintf("def %s():\n", name))
	s.Indent(2)
}

func (s *Starlark) MethodEnd() {
	s.Indent(-2)
	s.Write("\n")
}

func (s *Starlark) MethodCall(name, suffix string) {
	methodName := s.methodName(name, suffix)

	s.Write(fmt.Sprintf("%s(),\n", methodName))
}

func (s *Starlark) Return() {
	s.Write("return ")
}

func (s *Starlark) StartDict() {
	s.Write("{\n")
	s.Indent(2)
}

func (s *Starlark) DictFieldName(name string) {
	s.Write(fmt.Sprintf(`"%s": `, name))
}

func (s *Starlark) EndDict(comma bool) {
	s.Indent(-2)
	if comma {
		s.Write("},\n")
	} else {
		s.Write("}\n")
	}
}

func (s *Starlark) StartArray() {
	s.Indent(2)
	s.Write("[\n")
}

func (s *Starlark) EndArray() {
	s.Indent(-2)
	s.Write("],\n")
}

func (s *Starlark) Bytes() []byte {
	return s.buf.Bytes()
}

func (s *Starlark) String() string {
	return s.buf.String()
}

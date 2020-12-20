//+build windows

//revive:disable-next-line:var-naming
// Package win_eventlog Input plugin to collect Windows Event Log messages
package win_eventlog

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DecodeUTF16 to UTF8 bytes
func DecodeUTF16(b []byte) ([]byte, error) {

	if len(b)%2 != 0 {
		return nil, fmt.Errorf("must have even length byte slice")
	}

	u16s := make([]uint16, 1)

	ret := &bytes.Buffer{}

	b8buf := make([]byte, 4)

	lb := len(b)
	for i := 0; i < lb; i += 2 {
		u16s[0] = uint16(b[i]) + (uint16(b[i+1]) << 8)
		r := utf16.Decode(u16s)
		n := utf8.EncodeRune(b8buf, r[0])
		ret.Write(b8buf[:n])
	}

	return ret.Bytes(), nil
}

// GetFromSnapProcess finds information about process by the given pid
// Returns process parent pid, threads info handle and process name
func GetFromSnapProcess(pid uint32) (uint32, uint32, string, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, uint32(pid))
	if err != nil {
		return 0, 0, "", err
	}
	defer windows.CloseHandle(snap)
	var pe32 windows.ProcessEntry32
	pe32.Size = uint32(unsafe.Sizeof(pe32))
	if err = windows.Process32First(snap, &pe32); err != nil {
		return 0, 0, "", err
	}
	for {
		if pe32.ProcessID == uint32(pid) {
			szexe := windows.UTF16ToString(pe32.ExeFile[:])
			return uint32(pe32.ParentProcessID), uint32(pe32.Threads), szexe, nil
		}
		if err = windows.Process32Next(snap, &pe32); err != nil {
			break
		}
	}
	return 0, 0, "", fmt.Errorf("couldn't find pid: %d", pid)
}

type xmlnode struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:"-"`
	Content []byte     `xml:",innerxml"`
	Text    string     `xml:",chardata"`
	Nodes   []xmlnode  `xml:",any"`
}

// EventField for unique rendering
type EventField struct {
	Name  string
	Value string
}

// UnmarshalXML redefined for xml elements walk
func (n *xmlnode) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	n.Attrs = start.Attr
	type node xmlnode

	return d.DecodeElement((*node)(n), &start)
}

// UnrollXMLFields extracts fields from xml data
func UnrollXMLFields(data []byte, fieldsUsage map[string]int, separator string) ([]EventField, map[string]int) {
	buf := bytes.NewBuffer(data)
	dec := xml.NewDecoder(buf)
	var fields []EventField
	for {
		var node xmlnode
		err := dec.Decode(&node)
		if err == io.EOF {
			break
		}
		if err != nil {
			// log.Fatal(err)
			break
		}
		var parents []string
		walkXML([]xmlnode{node}, parents, separator, func(node xmlnode, parents []string, separator string) bool {
			innerText := strings.TrimSpace(node.Text)
			if len(innerText) > 0 {
				valueName := strings.Join(parents, separator)
				fieldsUsage[valueName]++
				field := EventField{Name: valueName, Value: innerText}
				fields = append(fields, field)
			}
			return true
		})
	}
	return fields, fieldsUsage
}

func walkXML(nodes []xmlnode, parents []string, separator string, f func(xmlnode, []string, string) bool) {
	for _, node := range nodes {
		parentName := node.XMLName.Local
		for _, attr := range node.Attrs {
			attrName := strings.ToLower(attr.Name.Local)
			if attrName == "name" {
				// Add Name attribute to parent name
				parentName = strings.Join([]string{parentName, attr.Value}, separator)
			}
		}
		nodeParents := append(parents, parentName)
		if f(node, nodeParents, separator) {
			walkXML(node.Nodes, nodeParents, separator, f)
		}
	}
}

// UniqueFieldNames forms unique field names
// by adding _<num> if there are several of them
func UniqueFieldNames(fields []EventField, fieldsUsage map[string]int, separator string) []EventField {
	var fieldsCounter = map[string]int{}
	var fieldsUnique []EventField
	for _, field := range fields {
		fieldName := field.Name
		if fieldsUsage[field.Name] > 1 {
			fieldsCounter[field.Name]++
			fieldName = fmt.Sprint(field.Name, separator, fieldsCounter[field.Name])
		}
		fieldsUnique = append(fieldsUnique, EventField{
			Name:  fieldName,
			Value: field.Value,
		})
	}
	return fieldsUnique
}

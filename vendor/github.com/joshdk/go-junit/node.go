// Copyright Josh Komoroske. All rights reserved.
// Use of this source code is governed by the MIT license,
// a copy of which can be found in the LICENSE.txt file.

package junit

import "encoding/xml"

type xmlNode struct {
	XMLName xml.Name
	Attrs   map[string]string `xml:"-"`
	Content []byte            `xml:",innerxml"`
	Nodes   []xmlNode         `xml:",any"`
}

func (n *xmlNode) Attr(name string) string {
	return n.Attrs[name]
}

func (n *xmlNode) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	type nodeAlias xmlNode
	if err := d.DecodeElement((*nodeAlias)(n), &start); err != nil {
		return err
	}

	content, err := extractContent(n.Content)
	if err != nil {
		return err
	}

	n.Content = content

	n.Attrs = attrMap(start.Attr)
	return nil
}

func attrMap(attrs []xml.Attr) map[string]string {
	if len(attrs) == 0 {
		return nil
	}

	attributes := make(map[string]string, len(attrs))
	for _, attr := range attrs {
		attributes[attr.Name.Local] = attr.Value
	}
	return attributes
}

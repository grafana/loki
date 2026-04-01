// Copyright Josh Komoroske. All rights reserved.
// Use of this source code is governed by the MIT license,
// a copy of which can be found in the LICENSE.txt file.

package junit

import (
	"bytes"
	"encoding/xml"
	"errors"
	"html"
	"io"
)

// reparentXML will wrap the given reader (which is assumed to be valid XML),
// in a fake root nodeAlias.
//
// This action is useful in the event that the original XML document does not
// have a single root nodeAlias, which is required by the XML specification.
// Additionally, Go's XML parser will silently drop all nodes after the first
// that is encountered, which can lead to data loss from a parser perspective.
// This function also enables the ingestion of blank XML files, which would
// normally cause a parsing error.
func reparentXML(reader io.Reader) io.Reader {
	return io.MultiReader(
		bytes.NewReader([]byte("<fake-root>")),
		reader,
		bytes.NewReader([]byte("</fake-root>")),
	)
}

// extractContent parses the raw contents from an XML node, and returns it in a
// more consumable form.
//
// This function deals with two distinct classes of node data; Encoded entities
// and CDATA tags. These Encoded entities are normal (html escaped) text that
// you typically find between tags like so:
//   • "Hello, world!"  →  "Hello, world!"
//   • "I &lt;/3 XML"   →  "I </3 XML"
// CDATA tags are a special way to embed data that would normally require
// escaping, without escaping it, like so:
//   • "<![CDATA[Hello, world!]]>"  →  "Hello, world!"
//   • "<![CDATA[I &lt;/3 XML]]>"   →  "I &lt;/3 XML"
//   • "<![CDATA[I </3 XML]]>"      →  "I </3 XML"
//
// This function specifically allows multiple interleaved instances of either
// encoded entities or cdata, and will decode them into one piece of normalized
// text, like so:
//   • "I &lt;/3 XML <![CDATA[a lot]]>. You probably <![CDATA[</3 XML]]> too."  →  "I </3 XML a lot. You probably </3 XML too."
//      └─────┬─────┘         └─┬─┘   └──────┬──────┘         └──┬──┘   └─┬─┘
//      "I </3 XML "            │     ". You probably "          │      " too."
//                          "a lot"                         "</3 XML"
//
// Errors are returned only when there are unmatched CDATA tags, although these
// should cause proper XML unmarshalling errors first, if encountered in an
// actual XML document.
func extractContent(data []byte) ([]byte, error) {
	var (
		cdataStart = []byte("<![CDATA[")
		cdataEnd   = []byte("]]>")
		mode       int
		output     []byte
	)

	for {
		if mode == 0 {
			offset := bytes.Index(data, cdataStart)
			if offset == -1 {
				// The string "<![CDATA[" does not appear in the data. Unescape all remaining data and finish
				if bytes.Contains(data, cdataEnd) {
					// The string "]]>" appears in the data. This is an error!
					return nil, errors.New("unmatched CDATA end tag")
				}

				output = append(output, html.UnescapeString(string(data))...)
				break
			}

			// The string "<![CDATA[" appears at some offset. Unescape up to that offset. Discard "<![CDATA[" prefix.
			output = append(output, html.UnescapeString(string(data[:offset]))...)
			data = data[offset:]
			data = data[9:]
			mode = 1
		} else if mode == 1 {
			offset := bytes.Index(data, cdataEnd)
			if offset == -1 {
				// The string "]]>" does not appear in the data. This is an error!
				return nil, errors.New("unmatched CDATA start tag")
			}

			// The string "]]>" appears at some offset. Read up to that offset. Discard "]]>" prefix.
			output = append(output, data[:offset]...)
			data = data[offset:]
			data = data[3:]
			mode = 0
		}
	}

	return output, nil
}

// parse unmarshalls the given XML data into a graph of nodes, and then returns
// a slice of all top-level nodes.
func parse(reader io.Reader) ([]xmlNode, error) {
	var (
		dec  = xml.NewDecoder(reparentXML(reader))
		root xmlNode
	)

	if err := dec.Decode(&root); err != nil {
		return nil, err
	}

	return root.Nodes, nil
}

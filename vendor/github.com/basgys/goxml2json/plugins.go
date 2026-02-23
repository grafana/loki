package xml2json

import (
	"strings"
)

type (
	// an plugin is added to an encoder or/and to an decoder to allow custom functionality at runtime
	plugin interface {
		AddToEncoder(*Encoder) *Encoder
		AddToDecoder(*Decoder) *Decoder
	}
	// a type converter overides the default string sanitization for encoding json
	encoderTypeConverter interface {
		Convert(string) string
	}
	// customTypeConverter converts strings to JSON types using a best guess approach, only parses the JSON types given
	// when initialized via WithTypeConverter
	customTypeConverter struct {
		parseTypes []JSType
	}

	attrPrefixer    string
	contentPrefixer string

	excluder []string

	nodesFormatter struct {
		list []nodeFormatter
	}
	nodeFormatter struct {
		path   string
		plugin nodePlugin
	}

	nodePlugin interface {
		AddTo(*Node)
	}

	arrayFormatter struct{}
)

// WithTypeConverter allows customized js type conversion behavior by passing in the desired JSTypes
func WithTypeConverter(ts ...JSType) *customTypeConverter {
	return &customTypeConverter{parseTypes: ts}
}

func (tc *customTypeConverter) parseAsString(t JSType) bool {
	if t == String {
		return true
	}
	for i := 0; i < len(tc.parseTypes); i++ {
		if tc.parseTypes[i] == t {
			return false
		}
	}
	return true
}

// Adds the type converter to the encoder
func (tc *customTypeConverter) AddToEncoder(e *Encoder) *Encoder {
	e.tc = tc
	return e
}

func (tc *customTypeConverter) AddToDecoder(d *Decoder) *Decoder {
	return d
}

func (tc *customTypeConverter) Convert(s string) string {
	// remove quotes if they exists
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		s = s[1 : len(s)-1]
	}
	jsType := Str2JSType(s)
	if tc.parseAsString(jsType) {
		// add the quotes removed at the start of this func
		s = `"` + s + `"`
	}
	return s
}

// WithAttrPrefix appends the given prefix to the json output of xml attribute fields to preserve namespaces
func WithAttrPrefix(prefix string) *attrPrefixer {
	ap := attrPrefixer(prefix)
	return &ap
}

func (a *attrPrefixer) AddToEncoder(e *Encoder) *Encoder {
	e.attributePrefix = string((*a))
	return e
}

func (a *attrPrefixer) AddToDecoder(d *Decoder) *Decoder {
	d.attributePrefix = string((*a))
	return d
}

// WithContentPrefix appends the given prefix to the json output of xml content fields to preserve namespaces
func WithContentPrefix(prefix string) *contentPrefixer {
	c := contentPrefixer(prefix)
	return &c
}

func (c *contentPrefixer) AddToEncoder(e *Encoder) *Encoder {
	e.contentPrefix = string((*c))
	return e
}

func (c *contentPrefixer) AddToDecoder(d *Decoder) *Decoder {
	d.contentPrefix = string((*c))
	return d
}

// ExcludeAttributes excludes some xml attributes, for example, xmlns:xsi, xsi:noNamespaceSchemaLocation
func ExcludeAttributes(attrs []string) *excluder {
	ex := excluder(attrs)
	return &ex
}

func (ex *excluder) AddToEncoder(e *Encoder) *Encoder {
	return e
}

func (ex *excluder) AddToDecoder(d *Decoder) *Decoder {
	d.ExcludeAttributes([]string((*ex)))
	return d
}

// WithNodes formats specific nodes
func WithNodes(n ...nodeFormatter) *nodesFormatter {
	return &nodesFormatter{list: n}
}

func (nf *nodesFormatter) AddToEncoder(e *Encoder) *Encoder {
	return e
}

func (nf *nodesFormatter) AddToDecoder(d *Decoder) *Decoder {
	d.AddFormatters(nf.list)
	return d
}

func NodePlugin(path string, plugin nodePlugin) nodeFormatter {
	return nodeFormatter{path: path, plugin: plugin}
}

func (nf *nodeFormatter) Format(node *Node) {
	child := node.GetChild(nf.path)
	if child != nil {
		nf.plugin.AddTo(child)
	}
}

func ToArray() *arrayFormatter {
	return &arrayFormatter{}
}

func (af *arrayFormatter) AddTo(n *Node) {
	n.ChildrenAlwaysAsArray = true
}

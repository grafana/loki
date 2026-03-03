package log

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

const (
	xmlSpacer = '_'
)

var (
	_ Stage = &XMLParser{}
)

type XMLParser struct {
	prefixBuffer    [][]byte // buffer used to build xml keys
	lbs             *LabelsBuilder
	captureXMLPath  bool
	stripNamespaces bool

	keys                  internedStringSet
	parserHints           ParserHint
	sanitizedPrefixBuffer []byte
}

// NewXMLParser creates a log stage that can parse an XML log line and add elements/attributes as labels.
// If stripNamespaces is true, XML namespaces will be removed from element/attribute names.
// If captureXMLPath is true, the XPath to each extracted field will be stored.
func NewXMLParser(stripNamespaces, captureXMLPath bool) *XMLParser {
	return &XMLParser{
		prefixBuffer:          [][]byte{},
		keys:                  internedStringSet{},
		captureXMLPath:        captureXMLPath,
		stripNamespaces:       stripNamespaces,
		sanitizedPrefixBuffer: make([]byte, 0, 64),
	}
}

func (x *XMLParser) Process(_ int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	parserHints := lbs.ParserLabelHints()
	if parserHints.NoLabels() {
		return line, true
	}

	// reset the state.
	x.prefixBuffer = x.prefixBuffer[:0]
	x.lbs = lbs
	x.parserHints = parserHints

	// Decode XML and process elements
	decoder := xml.NewDecoder(bytes.NewReader(line))
	decoder.CharsetReader = nil // Use default charset handling

	if err := x.parseXML(decoder); err != nil {
		if errors.Is(err, errFoundAllLabels) {
			// Short-circuited - all labels found
			return line, true
		}

		if errors.Is(err, errLabelDoesNotMatch) {
			// one of the label matchers does not match. The whole line can be thrown away
			return line, false
		}

		addErrLabel(errXML, err, lbs)
		return line, true
	}

	return line, true
}

func (x *XMLParser) parseXML(decoder *xml.Decoder) error {
	for {
		token, err := decoder.Token()
		if err != nil {
			// Expected end of document
			if err == io.EOF {
				return nil
			}
			return err
		}

		switch elem := token.(type) {
		case xml.StartElement:
			// Process element name and attributes
			name := x.stripNamespaceIfNeeded(elem.Name.Local)

			// Process element attributes
			if err := x.processElement(name, elem.Attr); err != nil {
				return err
			}

			// Add element to prefix for tracking nesting
			x.prefixBuffer = append(x.prefixBuffer, []byte(name))

		case xml.CharData:
			// Handle text content at current level
			text := bytes.TrimSpace(elem)
			if len(text) > 0 && len(x.prefixBuffer) > 0 {
				if err := x.processText(text); err != nil {
					return err
				}
			}

		case xml.EndElement:
			// Pop the element from prefix
			if len(x.prefixBuffer) > 0 {
				x.prefixBuffer = x.prefixBuffer[:len(x.prefixBuffer)-1]
			}
		}
	}
}

func (x *XMLParser) stripNamespaceIfNeeded(name string) string {
	if !x.stripNamespaces {
		return name
	}
	// Strip namespace prefix (everything before ':')
	if idx := strings.Index(name, ":"); idx != -1 {
		return name[idx+1:]
	}
	return name
}

func (x *XMLParser) processElement(name string, attrs []xml.Attr) error {
	// Process all attributes of this element
	for _, attr := range attrs {
		attrName := x.stripNamespaceIfNeeded(attr.Name.Local)

		// Build combined key: element_attribute
		combinedKey := name + "_" + attrName
		prefixLen := len(x.prefixBuffer)

		// Add to prefix temporarily to build full path
		x.prefixBuffer = append(x.prefixBuffer, []byte(combinedKey))

		sanitized := x.buildSanitizedPrefixFromBuffer()
		keyString, ok := x.keys.Get(sanitized, func() (string, bool) {
			field := sanitizeLabelKey(string(sanitized), true)
			if len(field) == 0 {
				return "", false
			}
			return field, true
		})

		if !ok {
			x.prefixBuffer = x.prefixBuffer[:prefixLen]
			continue
		}

		if x.lbs.BaseHas(keyString) || x.lbs.HasInCategory(keyString, StructuredMetadataLabel) {
			keyString = keyString + duplicateSuffix
		}

		if !x.lbs.ParserLabelHints().ShouldExtract(keyString) || x.lbs.ParserLabelHints().Extracted(keyString) {
			x.prefixBuffer = x.prefixBuffer[:prefixLen]
			continue
		}

		x.lbs.Set(ParsedLabel, keyString, attr.Value)
		if x.captureXMLPath {
			x.lbs.SetXMLPath(keyString, x.buildXMLPathFromPrefixBuffer())
		}

		if !x.parserHints.ShouldContinueParsingLine(keyString, x.lbs) {
			x.prefixBuffer = x.prefixBuffer[:prefixLen]
			return errLabelDoesNotMatch
		}

		x.prefixBuffer = x.prefixBuffer[:prefixLen]
	}

	return nil
}

func (x *XMLParser) processText(text []byte) error {
	if len(text) == 0 || len(x.prefixBuffer) == 0 {
		return nil
	}

	sanitized := x.buildSanitizedPrefixFromBuffer()
	if len(sanitized) == 0 {
		return nil
	}

	keyString, _ := x.keys.Get(sanitized, func() (string, bool) {
		return string(sanitized), true
	})

	if x.lbs.BaseHas(keyString) || x.lbs.HasInCategory(keyString, StructuredMetadataLabel) {
		keyString = keyString + duplicateSuffix
	}

	if !x.parserHints.ShouldExtract(keyString) || x.parserHints.Extracted(keyString) {
		return nil
	}

	textValue := string(text)
	if strings.ContainsRune(textValue, utf8.RuneError) {
		textValue = strings.Map(removeInvalidUtf, textValue)
	}

	x.lbs.Set(ParsedLabel, keyString, textValue)

	if x.captureXMLPath {
		x.lbs.SetXMLPath(keyString, x.buildXMLPathFromPrefixBuffer())
	}

	if !x.parserHints.ShouldContinueParsingLine(keyString, x.lbs) {
		return errLabelDoesNotMatch
	}

	if x.parserHints.AllRequiredExtracted() {
		return errFoundAllLabels
	}

	return nil
}

func (x *XMLParser) buildSanitizedPrefixFromBuffer() []byte {
	x.sanitizedPrefixBuffer = x.sanitizedPrefixBuffer[:0]

	for i, part := range x.prefixBuffer {
		if len(bytes.TrimSpace(part)) == 0 {
			continue
		}

		if i > 0 && len(x.sanitizedPrefixBuffer) > 0 {
			x.sanitizedPrefixBuffer = append(x.sanitizedPrefixBuffer, byte(xmlSpacer))
		}
		x.sanitizedPrefixBuffer = appendSanitized(x.sanitizedPrefixBuffer, part)
	}

	return x.sanitizedPrefixBuffer
}

func (x *XMLParser) buildXMLPathFromPrefixBuffer() []string {
	if len(x.prefixBuffer) == 0 {
		return nil
	}

	xmlPath := make([]string, 0, len(x.prefixBuffer))
	for _, part := range x.prefixBuffer {
		partStr := string(part)
		// Trim _extracted suffix if the extracted field was a duplicate field
		partStr = strings.TrimSuffix(partStr, duplicateSuffix)
		xmlPath = append(xmlPath, partStr)
	}

	return xmlPath
}

func (x *XMLParser) RequiredLabelNames() []string { return []string{} }

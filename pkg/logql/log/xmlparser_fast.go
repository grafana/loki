package log

import (
	"bytes"
	"errors"
	"strings"
	"unicode/utf8"
)

// FastXMLParser is an optimized XML parser that minimizes allocations
// It uses a simpler state machine instead of xml.Decoder to avoid unnecessary allocations
type FastXMLParser struct {
	prefixBuffer          [][]byte
	elementStack          [][]byte
	lbs                   *LabelsBuilder
	captureXMLPath        bool
	stripNamespaces       bool
	keys                  internedStringSet
	parserHints           ParserHint
	sanitizedPrefixBuffer []byte
	textBuffer            []byte
}

// NewFastXMLParser creates an optimized XML parser
func NewFastXMLParser(stripNamespaces, captureXMLPath bool) *FastXMLParser {
	return &FastXMLParser{
		prefixBuffer:          make([][]byte, 0, 8),
		elementStack:          make([][]byte, 0, 16),
		keys:                  internedStringSet{},
		captureXMLPath:        captureXMLPath,
		stripNamespaces:       stripNamespaces,
		sanitizedPrefixBuffer: make([]byte, 0, 64),
		textBuffer:            make([]byte, 0, 256),
	}
}

func (x *FastXMLParser) Process(_ int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	parserHints := lbs.ParserLabelHints()
	if parserHints.NoLabels() {
		return line, true
	}

	// Reset state
	x.prefixBuffer = x.prefixBuffer[:0]
	x.elementStack = x.elementStack[:0]
	x.textBuffer = x.textBuffer[:0]
	x.lbs = lbs
	x.parserHints = parserHints

	if err := x.parseSimpleXML(line); err != nil {
		if errors.Is(err, errFoundAllLabels) {
			return line, true
		}
		if errors.Is(err, errLabelDoesNotMatch) {
			return line, false
		}
		addErrLabel(errXML, err, lbs)
		return line, true
	}

	return line, true
}

// parseSimpleXML uses a simplified state machine to parse XML with fewer allocations
func (x *FastXMLParser) parseSimpleXML(data []byte) error {
	i := 0
	for i < len(data) {
		// Find next tag
		start := bytes.IndexByte(data[i:], '<')
		if start == -1 {
			// No more tags, check for trailing text
			if i < len(data) {
				text := bytes.TrimSpace(data[i:])
				if len(text) > 0 && len(x.elementStack) > 0 {
					if err := x.processText(text); err != nil {
						return err
					}
				}
			}
			break
		}

		i += start
		if i+1 >= len(data) {
			break
		}

		// Find end of tag
		end := bytes.IndexByte(data[i+1:], '>')
		if end == -1 {
			return errors.New("malformed XML: unclosed tag")
		}

		end += i + 1
		tag := data[i+1 : end]

		if len(tag) == 0 {
			i = end + 1
			continue
		}

		// Handle different tag types
		if bytes.HasPrefix(tag, []byte("!")) {
			// Declaration or CDATA
			if bytes.HasPrefix(tag, []byte("![CDATA[")) {
				// Find CDATA end
				cdataEnd := bytes.Index(data[end+1:], []byte("]]>"))
				if cdataEnd == -1 {
					return errors.New("malformed XML: unclosed CDATA")
				}
				text := data[end+1 : end+1+cdataEnd]
				if len(x.elementStack) > 0 {
					if err := x.processText(text); err != nil {
						return err
					}
				}
				i = end + 1 + cdataEnd + 3
				continue
			}
			// Skip declarations and comments
			i = end + 1
			continue
		}

		if bytes.HasPrefix(tag, []byte("?")) {
			// XML declaration
			i = end + 1
			continue
		}

		if bytes.HasPrefix(tag, []byte("/")) {
			// Closing tag
			if len(x.elementStack) > 0 {
				x.elementStack = x.elementStack[:len(x.elementStack)-1]
				x.prefixBuffer = x.prefixBuffer[:len(x.prefixBuffer)-1]
			}
			i = end + 1
			continue
		}

		// Opening tag
		selfClosing := bytes.HasSuffix(tag, []byte("/"))
		if selfClosing {
			tag = tag[:len(tag)-1]
		}

		// Parse tag and attributes
		parts := bytes.Fields(tag)
		if len(parts) == 0 {
			i = end + 1
			continue
		}

		elemName := x.stripNamespaceIfNeeded(string(parts[0]))
		elemNameBytes := []byte(elemName)

		// Process attributes BEFORE adding element to stack
		if len(parts) > 1 {
			if err := x.processAttributesParsed(elemNameBytes, tag); err != nil {
				return err
			}
		}

		// Add element to stack
		x.elementStack = append(x.elementStack, elemNameBytes)
		x.prefixBuffer = append(x.prefixBuffer, elemNameBytes)

		if !selfClosing {
			// Look for text content between this opening tag and the next tag
			textStart := end + 1
			nextTagPos := bytes.IndexByte(data[textStart:], '<')
			if nextTagPos > 0 {
				// There is text between this tag and the next
				text := bytes.TrimSpace(data[textStart : textStart+nextTagPos])
				if len(text) > 0 {
					if err := x.processText(text); err != nil {
						return err
					}
				}
			}
		}

		if selfClosing {
			x.elementStack = x.elementStack[:len(x.elementStack)-1]
			x.prefixBuffer = x.prefixBuffer[:len(x.prefixBuffer)-1]
		}

		i = end + 1
	}

	return nil
}

// processAttributesParsed extracts and processes attributes from a tag
func (x *FastXMLParser) processAttributesParsed(elemName []byte, tagData []byte) error {
	// Simple attribute parsing: look for name="value" patterns
	str := string(tagData[len(elemName):])

	// Split by spaces and look for = signs
	parts := strings.Fields(str)
	for _, part := range parts {
		if !strings.Contains(part, "=") {
			continue
		}

		idx := strings.Index(part, "=")
		if idx <= 0 || idx >= len(part)-1 {
			continue
		}

		attrName := part[:idx]
		attrValue := part[idx+1:]

		// Remove quotes
		if len(attrValue) >= 2 && attrValue[0] == '"' && attrValue[len(attrValue)-1] == '"' {
			attrValue = attrValue[1 : len(attrValue)-1]
		} else if len(attrValue) >= 2 && attrValue[0] == '\'' && attrValue[len(attrValue)-1] == '\'' {
			attrValue = attrValue[1 : len(attrValue)-1]
		}

		// Strip namespace from attribute name
		attrName = x.stripNamespaceIfNeeded(attrName)

		// Build key: element_attribute
		combinedKey := string(elemName) + "_" + attrName
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
			x.prefixBuffer = x.prefixBuffer[:len(x.prefixBuffer)-1]
			continue
		}

		if x.lbs.BaseHas(keyString) || x.lbs.HasInCategory(keyString, StructuredMetadataLabel) {
			keyString = keyString + duplicateSuffix
		}

		if !x.lbs.ParserLabelHints().ShouldExtract(keyString) || x.lbs.ParserLabelHints().Extracted(keyString) {
			x.prefixBuffer = x.prefixBuffer[:len(x.prefixBuffer)-1]
			continue
		}

		x.lbs.Set(ParsedLabel, keyString, attrValue)

		if !x.parserHints.ShouldContinueParsingLine(keyString, x.lbs) {
			x.prefixBuffer = x.prefixBuffer[:len(x.prefixBuffer)-1]
			return errLabelDoesNotMatch
		}

		x.prefixBuffer = x.prefixBuffer[:len(x.prefixBuffer)-1]
	}

	return nil
}

func (x *FastXMLParser) processText(text []byte) error {
	if len(text) == 0 || len(x.elementStack) == 0 {
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

	if !x.parserHints.ShouldContinueParsingLine(keyString, x.lbs) {
		return errLabelDoesNotMatch
	}

	if x.parserHints.AllRequiredExtracted() {
		return errFoundAllLabels
	}

	return nil
}

func (x *FastXMLParser) buildSanitizedPrefixFromBuffer() []byte {
	x.sanitizedPrefixBuffer = x.sanitizedPrefixBuffer[:0]

	for i, part := range x.elementStack {
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

func (x *FastXMLParser) stripNamespaceIfNeeded(name string) string {
	if !x.stripNamespaces {
		return name
	}
	if idx := strings.Index(name, ":"); idx != -1 {
		return name[idx+1:]
	}
	return name
}

func (x *FastXMLParser) RequiredLabelNames() []string { return []string{} }

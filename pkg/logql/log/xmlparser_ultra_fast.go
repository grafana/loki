package log

import (
	"bytes"
	"errors"
	"unicode/utf8"
)

// UltraFastXMLParser is an ultra-optimized XML parser designed for absolute speed
// It bypasses some of the generic infrastructure for maximum performance
type UltraFastXMLParser struct {
	prefixBuffer          [][]byte
	elementStack          [][]byte
	lbs                   *LabelsBuilder
	captureXMLPath        bool
	stripNamespaces       bool
	keys                  internedStringSet
	parserHints           ParserHint
	sanitizedPrefixBuffer []byte
	// Pre-allocated buffers for hot paths
	elementNameBuf        []byte
	attrNameBuf           []byte
	attrValueBuf          []byte
}

// NewUltraFastXMLParser creates an ultra-optimized XML parser
func NewUltraFastXMLParser(stripNamespaces, captureXMLPath bool) *UltraFastXMLParser {
	return &UltraFastXMLParser{
		prefixBuffer:          make([][]byte, 0, 8),
		elementStack:          make([][]byte, 0, 16),
		keys:                  internedStringSet{},
		captureXMLPath:        captureXMLPath,
		stripNamespaces:       stripNamespaces,
		sanitizedPrefixBuffer: make([]byte, 0, 64),
		elementNameBuf:        make([]byte, 0, 32),
		attrNameBuf:           make([]byte, 0, 32),
		attrValueBuf:          make([]byte, 0, 128),
	}
}

func (x *UltraFastXMLParser) RequiredLabelNames() []string { return []string{} }

func (x *UltraFastXMLParser) Process(_ int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	parserHints := lbs.ParserLabelHints()
	if parserHints.NoLabels() {
		return line, true
	}

	x.prefixBuffer = x.prefixBuffer[:0]
	x.elementStack = x.elementStack[:0]
	x.lbs = lbs
	x.parserHints = parserHints

	if err := x.parseXML(line); err != nil {
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

func (x *UltraFastXMLParser) parseXML(data []byte) error {
	i := 0

	for i < len(data) {
		// Find next '<'
		start := bytes.IndexByte(data[i:], '<')
		if start == -1 {
			// Check for trailing text
			if i < len(data) {
				text := bytes.TrimSpace(data[i:])
				if len(text) > 0 && len(x.elementStack) > 0 {
					if err := x.processTextFast(text); err != nil {
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

		// Find matching '>'
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

		// Quick filter for common prefixes
		first := tag[0]

		// Skip XML declaration and comments
		if first == '?' || first == '!' {
			i = end + 1
			continue
		}

		// Closing tag
		if first == '/' {
			if len(x.elementStack) > 0 {
				x.elementStack = x.elementStack[:len(x.elementStack)-1]
				x.prefixBuffer = x.prefixBuffer[:len(x.prefixBuffer)-1]
			}
			i = end + 1
			continue
		}

		// Opening tag - check for self-closing
		isSelfClosing := len(tag) > 0 && tag[len(tag)-1] == '/'
		if isSelfClosing {
			tag = tag[:len(tag)-1]
		}

		// Extract element name (fast path - no string allocation if possible)
		elemEnd := bytes.IndexByte(tag, ' ')
		if elemEnd == -1 {
			elemEnd = len(tag)
		}
		elemNameBytes := tag[:elemEnd]

		// Strip namespace if needed
		if x.stripNamespaces {
			if colonIdx := bytes.IndexByte(elemNameBytes, ':'); colonIdx != -1 {
				elemNameBytes = elemNameBytes[colonIdx+1:]
			}
		}

		// Process attributes if present
		if elemEnd < len(tag) {
			if err := x.processAttributesFast(elemNameBytes, tag); err != nil {
				return err
			}
		}

		// Add element to stacks
		x.elementStack = append(x.elementStack, elemNameBytes)
		x.prefixBuffer = append(x.prefixBuffer, elemNameBytes)

		// Process text content for non-self-closing tags
		if !isSelfClosing {
			textStart := end + 1
			nextTag := bytes.IndexByte(data[textStart:], '<')
			if nextTag > 0 {
				text := bytes.TrimSpace(data[textStart : textStart+nextTag])
				if len(text) > 0 {
					if err := x.processTextFast(text); err != nil {
						return err
					}
				}
			}
		} else {
			// Self-closing: pop immediately
			x.elementStack = x.elementStack[:len(x.elementStack)-1]
			x.prefixBuffer = x.prefixBuffer[:len(x.prefixBuffer)-1]
		}

		i = end + 1
	}

	return nil
}

func (x *UltraFastXMLParser) processAttributesFast(elemName, tagData []byte) error {
	// Skip element name and find attributes
	attrStart := bytes.IndexByte(tagData, ' ')
	if attrStart == -1 {
		return nil
	}

	// Parse attributes directly without string conversion when possible
	attrs := tagData[attrStart+1:]

	for len(attrs) > 0 {
		// Skip whitespace
		attrs = bytes.TrimLeft(attrs, " \t\n\r")
		if len(attrs) == 0 {
			break
		}

		// Find attribute name
		eqIdx := bytes.IndexByte(attrs, '=')
		if eqIdx == -1 {
			break
		}

		attrName := bytes.TrimSpace(attrs[:eqIdx])
		if len(attrName) == 0 {
			break
		}

		// Strip namespace from attribute name
		if colonIdx := bytes.IndexByte(attrName, ':'); colonIdx != -1 {
			if x.stripNamespaces {
				attrName = attrName[colonIdx+1:]
			}
		}

		// Skip = and find value
		attrs = attrs[eqIdx+1:]
		attrs = bytes.TrimLeft(attrs, " \t")
		if len(attrs) == 0 {
			break
		}

		// Get quoted value
		quote := attrs[0]
		if quote != '"' && quote != '\'' {
			break
		}

		attrs = attrs[1:]
		endQuote := bytes.IndexByte(attrs, quote)
		if endQuote == -1 {
			break
		}

		attrValue := attrs[:endQuote]
		attrs = attrs[endQuote+1:]

		// Build combined key: element_attribute
		x.prefixBuffer = append(x.prefixBuffer, attrName)
		sanitized := x.buildSanitizedPrefixFast()

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

		// Convert attribute value to string
		attrValueStr := string(attrValue)
		x.lbs.Set(ParsedLabel, keyString, attrValueStr)

		if !x.parserHints.ShouldContinueParsingLine(keyString, x.lbs) {
			x.prefixBuffer = x.prefixBuffer[:len(x.prefixBuffer)-1]
			return errLabelDoesNotMatch
		}

		x.prefixBuffer = x.prefixBuffer[:len(x.prefixBuffer)-1]
	}

	return nil
}

func (x *UltraFastXMLParser) processTextFast(text []byte) error {
	sanitized := x.buildSanitizedPrefixFast()
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
	if bytes.ContainsRune(text, utf8.RuneError) {
		textValue = string(bytes.Map(removeInvalidUtf, text))
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

// buildSanitizedPrefixFast - optimized version without the generic infrastructure
func (x *UltraFastXMLParser) buildSanitizedPrefixFast() []byte {
	x.sanitizedPrefixBuffer = x.sanitizedPrefixBuffer[:0]

	for i, part := range x.prefixBuffer {
		if len(bytes.TrimSpace(part)) == 0 {
			continue
		}

		if i > 0 && len(x.sanitizedPrefixBuffer) > 0 {
			x.sanitizedPrefixBuffer = append(x.sanitizedPrefixBuffer, '_')
		}
		x.sanitizedPrefixBuffer = appendSanitized(x.sanitizedPrefixBuffer, part)
	}

	return x.sanitizedPrefixBuffer
}

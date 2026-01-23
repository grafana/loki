package log

import (
	"bytes"
	"encoding/xml"
	"errors"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

type XMLUnpackParser struct {
	lbsBuffer []string // Buffer for label key-value pairs
	keys      internedStringSet
}

// NewXMLUnpackParser creates a new XML unpack stage.
// The unpack stage will parse an XML log line where elements are treated as a map[string]string.
// Each element name will be translated into a label. A special key _entry can be used to replace
// the original log line. This is to be used in conjunction with Promtail pack stage.
func NewXMLUnpackParser() *XMLUnpackParser {
	return &XMLUnpackParser{
		lbsBuffer: make([]string, 0, 16),
		keys:      internedStringSet{},
	}
}

func (XMLUnpackParser) RequiredLabelNames() []string { return []string{} }

func (x *XMLUnpackParser) Process(_ int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	if len(line) == 0 || lbs.ParserLabelHints().NoLabels() {
		return line, true
	}

	// XML should start with '<'
	if line[0] != '<' {
		addErrLabel(errXML, errors.New("expected XML"), lbs)
		return line, true
	}

	x.lbsBuffer = x.lbsBuffer[:0]
	entry, err := x.unpack(line, lbs)
	if err != nil {
		if errors.Is(err, errLabelDoesNotMatch) {
			return entry, false
		}
		addErrLabel(errXML, err, lbs)
		return line, true
	}

	return entry, true
}

func (x *XMLUnpackParser) unpack(entry []byte, lbs *LabelsBuilder) ([]byte, error) {
	// Parse XML
	decoder := xml.NewDecoder(bytes.NewReader(entry))

	for {
		token, err := decoder.Token()
		if err != nil {
			// EOF is expected
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}

		elem, ok := token.(xml.StartElement)
		if !ok {
			continue
		}

		name := elem.Name.Local

		// Check for special _entry key
		if name == logqlmodel.PackedEntryKey {
			// For XML, we look for the text content of this element
			for {
				t, err := decoder.Token()
				if err != nil {
					break
				}
				if cd, ok := t.(xml.CharData); ok {
					entry = bytes.TrimSpace(cd)
				}
				if _, ok := t.(xml.EndElement); ok {
					break
				}
			}
			continue
		}

		// Look for character data in this element
		var charData []byte
		for {
			t, err := decoder.Token()
			if err != nil {
				break
			}
			switch v := t.(type) {
			case xml.CharData:
				charData = bytes.TrimSpace(v)
			case xml.EndElement:
				// Process the element
				if len(charData) > 0 {
					key, _ := x.keys.Get([]byte(name), func() (string, bool) {
						return name, true
					})

					if lbs.BaseHas(key) || lbs.HasInCategory(key, StructuredMetadataLabel) {
						key = key + duplicateSuffix
					}

					if !lbs.ParserLabelHints().ShouldExtract(key) || lbs.ParserLabelHints().Extracted(key) {
						break
					}

					// Append to the buffer of labels
					x.lbsBuffer = append(x.lbsBuffer, sanitizeLabelKey(key, true), string(charData))
				}
				break
			}
		}
	}

	// Flush the buffer (always flush, not just if isPacked)
	for i := 0; i < len(x.lbsBuffer); i = i + 2 {
		lbs.Set(ParsedLabel, x.lbsBuffer[i], x.lbsBuffer[i+1])
		if !lbs.ParserLabelHints().ShouldContinueParsingLine(x.lbsBuffer[i], lbs) {
			return entry, errLabelDoesNotMatch
		}
	}

	return entry, nil
}

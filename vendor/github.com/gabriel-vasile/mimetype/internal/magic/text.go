package magic

import (
	"bytes"
	"time"

	"github.com/gabriel-vasile/mimetype/internal/charset"
	"github.com/gabriel-vasile/mimetype/internal/json"
	mkup "github.com/gabriel-vasile/mimetype/internal/markup"
	"github.com/gabriel-vasile/mimetype/internal/scan"
)

// HTML matches a Hypertext Markup Language file.
func HTML(raw []byte, _ uint32) bool {
	return markup(raw,
		[]byte("<!DOCTYPE HTML"),
		[]byte("<HTML"),
		[]byte("<HEAD"),
		[]byte("<SCRIPT"),
		[]byte("<IFRAME"),
		[]byte("<H1"),
		[]byte("<DIV"),
		[]byte("<FONT"),
		[]byte("<TABLE"),
		[]byte("<A"),
		[]byte("<STYLE"),
		[]byte("<TITLE"),
		[]byte("<B"),
		[]byte("<BODY"),
		[]byte("<BR"),
		[]byte("<P"),
		[]byte("<!--"),
	)
}

// XML matches an Extensible Markup Language file.
func XML(raw []byte, _ uint32) bool {
	return markup(raw, []byte("<?XML"))
}

// Owl2 matches an Owl ontology file.
func Owl2(raw []byte, _ uint32) bool {
	return xml(raw,
		xmlSig{[]byte("<Ontology"), []byte(`xmlns="http://www.w3.org/2002/07/owl#"`)},
	)
}

// Rss matches a Rich Site Summary file.
func Rss(raw []byte, _ uint32) bool {
	return xml(raw,
		xmlSig{[]byte("<rss"), []byte{}},
	)
}

// Atom matches an Atom Syndication Format file.
func Atom(raw []byte, _ uint32) bool {
	return xml(raw,
		xmlSig{[]byte("<feed"), []byte(`xmlns="http://www.w3.org/2005/Atom"`)},
	)
}

// Kml matches a Keyhole Markup Language file.
func Kml(raw []byte, _ uint32) bool {
	return xml(raw,
		xmlSig{[]byte("<kml"), []byte(`xmlns="http://www.opengis.net/kml/2.2"`)},
		xmlSig{[]byte("<kml"), []byte(`xmlns="http://earth.google.com/kml/2.0"`)},
		xmlSig{[]byte("<kml"), []byte(`xmlns="http://earth.google.com/kml/2.1"`)},
		xmlSig{[]byte("<kml"), []byte(`xmlns="http://earth.google.com/kml/2.2"`)},
	)
}

// Xliff matches a XML Localization Interchange File Format file.
func Xliff(raw []byte, _ uint32) bool {
	return xml(raw,
		xmlSig{[]byte("<xliff"), []byte(`xmlns="urn:oasis:names:tc:xliff:document:1.2"`)},
	)
}

// Collada matches a COLLAborative Design Activity file.
func Collada(raw []byte, _ uint32) bool {
	return xml(raw,
		xmlSig{[]byte("<COLLADA"), []byte(`xmlns="http://www.collada.org/2005/11/COLLADASchema"`)},
	)
}

// Gml matches a Geography Markup Language file.
func Gml(raw []byte, _ uint32) bool {
	return xml(raw,
		xmlSig{[]byte{}, []byte(`xmlns:gml="http://www.opengis.net/gml"`)},
		xmlSig{[]byte{}, []byte(`xmlns:gml="http://www.opengis.net/gml/3.2"`)},
		xmlSig{[]byte{}, []byte(`xmlns:gml="http://www.opengis.net/gml/3.3/exr"`)},
	)
}

// Gpx matches a GPS Exchange Format file.
func Gpx(raw []byte, _ uint32) bool {
	return xml(raw,
		xmlSig{[]byte("<gpx"), []byte(`xmlns="http://www.topografix.com/GPX/1/1"`)},
	)
}

// Tcx matches a Training Center XML file.
func Tcx(raw []byte, _ uint32) bool {
	return xml(raw,
		xmlSig{[]byte("<TrainingCenterDatabase"), []byte(`xmlns="http://www.garmin.com/xmlschemas/TrainingCenterDatabase/v2"`)},
	)
}

// X3d matches an Extensible 3D Graphics file.
func X3d(raw []byte, _ uint32) bool {
	return xml(raw,
		xmlSig{[]byte("<X3D"), []byte(`xmlns:xsd="http://www.w3.org/2001/XMLSchema-instance"`)},
	)
}

// Amf matches an Additive Manufacturing XML file.
func Amf(raw []byte, _ uint32) bool {
	return xml(raw, xmlSig{[]byte("<amf"), []byte{}})
}

// Threemf matches a 3D Manufacturing Format file.
func Threemf(raw []byte, _ uint32) bool {
	return xml(raw,
		xmlSig{[]byte("<model"), []byte(`xmlns="http://schemas.microsoft.com/3dmanufacturing/core/2015/02"`)},
	)
}

// Xfdf matches a XML Forms Data Format file.
func Xfdf(raw []byte, _ uint32) bool {
	return xml(raw, xmlSig{[]byte("<xfdf"), []byte(`xmlns="http://ns.adobe.com/xfdf/"`)})
}

// VCard matches a Virtual Contact File.
func VCard(raw []byte, _ uint32) bool {
	return ciPrefix(raw, []byte("BEGIN:VCARD\n"), []byte("BEGIN:VCARD\r\n"))
}

// ICalendar matches a iCalendar file.
func ICalendar(raw []byte, _ uint32) bool {
	return ciPrefix(raw, []byte("BEGIN:VCALENDAR\n"), []byte("BEGIN:VCALENDAR\r\n"))
}
func phpPageF(raw []byte, _ uint32) bool {
	return ciPrefix(raw,
		[]byte("<?PHP"),
		[]byte("<?\n"),
		[]byte("<?\r"),
		[]byte("<? "),
	)
}
func phpScriptF(raw []byte, _ uint32) bool {
	return shebang(raw,
		scan.CompactWS,
		[]byte("/usr/local/bin/php"),
		[]byte("/usr/bin/php"),
		[]byte("/usr/bin/env php"),
		[]byte("/usr/bin/env -S php"),
	)
}

// Js matches a Javascript file.
func Js(raw []byte, _ uint32) bool {
	return shebang(raw,
		scan.CompactWS,
		[]byte("/bin/node"),
		[]byte("/usr/bin/node"),
		[]byte("/bin/nodejs"),
		[]byte("/usr/bin/nodejs"),
		[]byte("/usr/bin/env node"),
		[]byte("/usr/bin/env -S node"),
		[]byte("/usr/bin/env nodejs"),
		[]byte("/usr/bin/env -S nodejs"),
	)
}

// Lua matches a Lua programming language file.
func Lua(raw []byte, _ uint32) bool {
	return shebang(raw,
		scan.CompactWS|scan.FullWord,
		[]byte("/usr/bin/lua"),
		[]byte("/usr/local/bin/lua"),
		[]byte("/usr/bin/env lua"),
		[]byte("/usr/bin/env -S lua"),
	)
}

// Perl matches a Perl programming language file.
func Perl(raw []byte, _ uint32) bool {
	return shebang(raw,
		scan.CompactWS|scan.FullWord,
		[]byte("/usr/bin/perl"),
		[]byte("/usr/bin/env perl"),
		[]byte("/usr/bin/env -S perl"),
	)
}

// Python matches a Python programming language file.
func Python(raw []byte, _ uint32) bool {
	return shebang(raw,
		scan.CompactWS,
		[]byte("/usr/bin/python"),
		[]byte("/usr/local/bin/python"),
		[]byte("/usr/bin/env python"),
		[]byte("/usr/bin/env -S python"),
		[]byte("/usr/bin/python2"),
		[]byte("/usr/local/bin/python2"),
		[]byte("/usr/bin/env python2"),
		[]byte("/usr/bin/env -S python2"),
		[]byte("/usr/bin/python3"),
		[]byte("/usr/local/bin/python3"),
		[]byte("/usr/bin/env python3"),
		[]byte("/usr/bin/env -S python3"),
	)

}

// Ruby matches a Ruby programming language file.
func Ruby(raw []byte, _ uint32) bool {
	return shebang(raw,
		scan.CompactWS,
		[]byte("/usr/bin/ruby"),
		[]byte("/usr/local/bin/ruby"),
		[]byte("/usr/bin/env ruby"),
		[]byte("/usr/bin/env -S ruby"),
	)
}

// Tcl matches a Tcl programming language file.
func Tcl(raw []byte, _ uint32) bool {
	return shebang(raw,
		scan.CompactWS,
		[]byte("/usr/bin/tcl"),
		[]byte("/usr/local/bin/tcl"),
		[]byte("/usr/bin/env tcl"),
		[]byte("/usr/bin/env -S tcl"),
		[]byte("/usr/bin/tclsh"),
		[]byte("/usr/local/bin/tclsh"),
		[]byte("/usr/bin/env tclsh"),
		[]byte("/usr/bin/env -S tclsh"),
		[]byte("/usr/bin/wish"),
		[]byte("/usr/local/bin/wish"),
		[]byte("/usr/bin/env wish"),
		[]byte("/usr/bin/env -S wish"),
	)
}

// Rtf matches a Rich Text Format file.
func Rtf(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte("{\\rtf"))
}

// Shell matches a shell script file.
func Shell(raw []byte, _ uint32) bool {
	return shebang(raw,
		scan.CompactWS|scan.FullWord,
		[]byte("/bin/sh"),
		[]byte("/bin/bash"),
		[]byte("/usr/local/bin/bash"),
		[]byte("/usr/bin/env bash"),
		[]byte("/usr/bin/env -S bash"),
		[]byte("/bin/csh"),
		[]byte("/usr/local/bin/csh"),
		[]byte("/usr/bin/env csh"),
		[]byte("/usr/bin/env -S csh"),
		[]byte("/bin/dash"),
		[]byte("/usr/local/bin/dash"),
		[]byte("/usr/bin/env dash"),
		[]byte("/usr/bin/env -S dash"),
		[]byte("/bin/ksh"),
		[]byte("/usr/local/bin/ksh"),
		[]byte("/usr/bin/env ksh"),
		[]byte("/usr/bin/env -S ksh"),
		[]byte("/bin/tcsh"),
		[]byte("/usr/local/bin/tcsh"),
		[]byte("/usr/bin/env tcsh"),
		[]byte("/usr/bin/env -S tcsh"),
		[]byte("/bin/zsh"),
		[]byte("/usr/local/bin/zsh"),
		[]byte("/usr/bin/env zsh"),
		[]byte("/usr/bin/env -S zsh"),
	)
}

// Text matches a plain text file.
//
// TODO: This function does not parse BOM-less UTF16 and UTF32 files. Not really
// sure it should. Linux file utility also requires a BOM for UTF16 and UTF32.
func Text(raw []byte, _ uint32) bool {
	// First look for BOM.
	if cset := charset.FromBOM(raw); cset != "" {
		return true
	}
	// Binary data bytes as defined here: https://mimesniff.spec.whatwg.org/#binary-data-byte
	for i := 0; i < min(len(raw), 4096); i++ {
		b := raw[i]
		if b <= 0x08 ||
			b == 0x0B ||
			0x0E <= b && b <= 0x1A ||
			0x1C <= b && b <= 0x1F {
			return false
		}
	}
	return true
}

// XHTML matches an XHTML file. This check depends on the XML check to have passed.
func XHTML(raw []byte, limit uint32) bool {
	raw = raw[:min(len(raw), 1024)]
	b := scan.Bytes(raw)
	i, _ := b.Search([]byte("<!DOCTYPE HTML"), scan.CompactWS|scan.IgnoreCase)
	if i != -1 {
		return true
	}
	i, _ = b.Search([]byte("<HTML XMLNS="), scan.CompactWS|scan.IgnoreCase)
	return i != -1
}

// Php matches a PHP: Hypertext Preprocessor file.
func Php(raw []byte, limit uint32) bool {
	if res := phpPageF(raw, limit); res {
		return res
	}
	return phpScriptF(raw, limit)
}

// JSON matches a JavaScript Object Notation file.
func JSON(raw []byte, limit uint32) bool {
	// #175 A single JSON string, number or bool is not considered JSON.
	// JSON objects and arrays are reported as JSON.
	return jsonHelper(raw, limit, json.QueryNone, json.TokObject|json.TokArray)
}

// GeoJSON matches a RFC 7946 GeoJSON file.
//
// GeoJSON detection implies searching for key:value pairs like: `"type": "Feature"`
// in the input.
func GeoJSON(raw []byte, limit uint32) bool {
	return jsonHelper(raw, limit, json.QueryGeo, json.TokObject)
}

// HAR matches a HAR Spec file.
// Spec: http://www.softwareishard.com/blog/har-12-spec/
func HAR(raw []byte, limit uint32) bool {
	return jsonHelper(raw, limit, json.QueryHAR, json.TokObject)
}

// GLTF matches a GL Transmission Format (JSON) file.
// Visit [glTF specification] and [IANA glTF entry] for more details.
//
// [glTF specification]: https://registry.khronos.org/glTF/specs/2.0/glTF-2.0.html
// [IANA glTF entry]: https://www.iana.org/assignments/media-types/model/gltf+json
func GLTF(raw []byte, limit uint32) bool {
	return jsonHelper(raw, limit, json.QueryGLTF, json.TokObject)
}

func jsonHelper(raw []byte, limit uint32, q string, wantTok int) bool {
	if !json.LooksLikeObjectOrArray(raw) {
		return false
	}
	lraw := len(raw)
	parsed, inspected, firstToken, querySatisfied := json.Parse(q, raw)
	if !querySatisfied || firstToken&wantTok == 0 {
		return false
	}

	// If the full file content was provided, check that the whole input was parsed.
	if limit == 0 || lraw < int(limit) {
		return parsed == lraw
	}

	// If a section of the file was provided, check if all of it was inspected.
	// In other words, check that if there was a problem parsing, that problem
	// occured at the last byte in the input.
	return inspected == lraw && lraw > 0
}

// NdJSON matches a Newline delimited JSON file. All complete lines from raw
// must be valid JSON documents meaning they contain one of the valid JSON data
// types.
func NdJSON(raw []byte, limit uint32) bool {
	lCount, objOrArr := 0, 0

	s := scan.Bytes(raw)
	s.DropLastLine(limit)
	var l scan.Bytes
	for len(s) != 0 {
		l = s.Line()
		_, inspected, firstToken, _ := json.Parse(json.QueryNone, l)
		if len(l) != inspected {
			return false
		}
		if firstToken == json.TokArray || firstToken == json.TokObject {
			objOrArr++
		}
		lCount++
	}

	return lCount > 1 && objOrArr > 0
}

// Svg matches a SVG file.
func Svg(raw []byte, limit uint32) bool {
	return svgWithoutXMLDeclaration(raw) || svgWithXMLDeclaration(raw)
}

// svgWithoutXMLDeclaration matches a SVG image that does not have an XML header.
// Example:
//
//	<!-- xml comment ignored -->
//	<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink">
//	    <rect fill="#fff" stroke="#000" x="-70" y="-70" width="390" height="390"/>
//	</svg>
func svgWithoutXMLDeclaration(s scan.Bytes) bool {
	for scan.ByteIsWS(s.Peek()) {
		s.Advance(1)
	}
	for mkup.SkipAComment(&s) {
	}
	if !bytes.HasPrefix(s, []byte("<svg")) {
		return false
	}

	targetName, targetVal := []byte("xmlns"), []byte("http://www.w3.org/2000/svg")
	var aName, aVal []byte
	hasMore := true
	for hasMore {
		aName, aVal, hasMore = mkup.GetAnAttribute(&s)
		if bytes.Equal(aName, targetName) && bytes.Equal(aVal, targetVal) {
			return true
		}
		if !hasMore {
			return false
		}
	}
	return false
}

// svgWithXMLDeclaration matches a SVG image that has an XML header.
// Example:
//
//	<?xml version="1.0" encoding="UTF-8" standalone="no"?>
//	<svg width="391" height="391" viewBox="-70.5 -70.5 391 391" xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink">
//	    <rect fill="#fff" stroke="#000" x="-70" y="-70" width="390" height="390"/>
//	</svg>
func svgWithXMLDeclaration(s scan.Bytes) bool {
	for scan.ByteIsWS(s.Peek()) {
		s.Advance(1)
	}
	if !bytes.HasPrefix(s, []byte("<?xml")) {
		return false
	}

	// version is a required attribute for XML.
	hasVersion := false
	var aName []byte
	hasMore := true
	for hasMore {
		aName, _, hasMore = mkup.GetAnAttribute(&s)
		if bytes.Equal(aName, []byte("version")) {
			hasVersion = true
			break
		}
		if !hasMore {
			break
		}
	}
	if len(s) > 4096 {
		s = s[:4096]
	}
	return hasVersion && bytes.Contains(s, []byte("<svg"))
}

// Srt matches a SubRip file.
func Srt(raw []byte, _ uint32) bool {
	s := scan.Bytes(raw)
	line := s.Line()

	// First line must be 1.
	if len(line) != 1 || line[0] != '1' {
		return false
	}
	line = s.Line()
	// Timestamp format (e.g: 00:02:16,612 --> 00:02:19,376) limits second line
	// length to exactly 29 characters.
	if len(line) != 29 {
		return false
	}
	// Decimal separator of fractional seconds in the timestamps must be a
	// comma, not a period.
	if bytes.IndexByte(line, '.') != -1 {
		return false
	}
	sep := []byte(" --> ")
	i := bytes.Index(line, sep)
	if i == -1 {
		return false
	}
	const layout = "15:04:05,000"
	t0, err := time.Parse(layout, string(line[:i]))
	if err != nil {
		return false
	}
	t1, err := time.Parse(layout, string(line[i+len(sep):]))
	if err != nil {
		return false
	}
	if t0.After(t1) {
		return false
	}

	line = s.Line()
	// A third line must exist and not be empty. This is the actual subtitle text.
	return len(line) != 0
}

// Vtt matches a Web Video Text Tracks (WebVTT) file. See
// https://www.iana.org/assignments/media-types/text/vtt.
func Vtt(raw []byte, limit uint32) bool {
	// Prefix match.
	prefixes := [][]byte{
		{0xEF, 0xBB, 0xBF, 0x57, 0x45, 0x42, 0x56, 0x54, 0x54, 0x0A}, // UTF-8 BOM, "WEBVTT" and a line feed
		{0xEF, 0xBB, 0xBF, 0x57, 0x45, 0x42, 0x56, 0x54, 0x54, 0x0D}, // UTF-8 BOM, "WEBVTT" and a carriage return
		{0xEF, 0xBB, 0xBF, 0x57, 0x45, 0x42, 0x56, 0x54, 0x54, 0x20}, // UTF-8 BOM, "WEBVTT" and a space
		{0xEF, 0xBB, 0xBF, 0x57, 0x45, 0x42, 0x56, 0x54, 0x54, 0x09}, // UTF-8 BOM, "WEBVTT" and a horizontal tab
		{0x57, 0x45, 0x42, 0x56, 0x54, 0x54, 0x0A},                   // "WEBVTT" and a line feed
		{0x57, 0x45, 0x42, 0x56, 0x54, 0x54, 0x0D},                   // "WEBVTT" and a carriage return
		{0x57, 0x45, 0x42, 0x56, 0x54, 0x54, 0x20},                   // "WEBVTT" and a space
		{0x57, 0x45, 0x42, 0x56, 0x54, 0x54, 0x09},                   // "WEBVTT" and a horizontal tab
	}
	for _, p := range prefixes {
		if bytes.HasPrefix(raw, p) {
			return true
		}
	}

	// Exact match.
	return bytes.Equal(raw, []byte{0xEF, 0xBB, 0xBF, 0x57, 0x45, 0x42, 0x56, 0x54, 0x54}) || // UTF-8 BOM and "WEBVTT"
		bytes.Equal(raw, []byte{0x57, 0x45, 0x42, 0x56, 0x54, 0x54}) // "WEBVTT"
}

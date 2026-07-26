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

// CDXXML matches a CycloneDX XML BOM file.
// https://cyclonedx.org/docs/1.7/xml/
func CDXXML(raw []byte, _ uint32) bool {
	// xmlns is missing the version suffix because there are too many past versions
	// and probably future versions to come.
	return xml(raw, xmlSig{[]byte("<bom"), []byte(`xmlns="http://cyclonedx.org/schema/bom/`)})
}

// VCard matches a Virtual Contact File.
func VCard(raw []byte, _ uint32) bool {
	return ciPrefix(raw, []byte("BEGIN:VCARD\n"), []byte("BEGIN:VCARD\r\n"))
}

// ICalendar matches a iCalendar file.
func ICalendar(raw []byte, _ uint32) bool {
	return ciPrefix(raw, []byte("BEGIN:VCALENDAR\n"), []byte("BEGIN:VCALENDAR\r\n"))
}

const (
	snone  = 0
	scws   = scan.CompactWS
	sfw    = scan.FullWord
	scwsfw = scan.CompactWS | scan.FullWord
)

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
		shebangSig{[]byte("/usr/local/bin/php"), snone},
		shebangSig{[]byte("/usr/bin/php"), snone},
		shebangSig{[]byte("/usr/bin/env php"), scws},
		shebangSig{[]byte("/usr/bin/env -S php"), scws},
	)
}

// Js matches a Javascript file.
func Js(raw []byte, _ uint32) bool {
	return shebang(raw,
		shebangSig{[]byte("/bin/node"), snone},
		shebangSig{[]byte("/usr/bin/node"), snone},
		shebangSig{[]byte("/bin/nodejs"), snone},
		shebangSig{[]byte("/usr/bin/nodejs"), snone},
		shebangSig{[]byte("/usr/bin/env node"), scws},
		shebangSig{[]byte("/usr/bin/env -S node"), scws},
		shebangSig{[]byte("/usr/bin/env nodejs"), scws},
		shebangSig{[]byte("/usr/bin/env -S nodejs"), scws},
	)
}

// Lua matches a Lua programming language file.
func Lua(raw []byte, _ uint32) bool {
	return shebang(raw,
		shebangSig{[]byte("/usr/bin/lua"), sfw},
		shebangSig{[]byte("/usr/local/bin/lua"), sfw},
		shebangSig{[]byte("/usr/bin/env lua"), scwsfw},
		shebangSig{[]byte("/usr/bin/env -S lua"), scwsfw},
	)
}

// Perl matches a Perl programming language file.
func Perl(raw []byte, _ uint32) bool {
	return shebang(raw,
		shebangSig{[]byte("/usr/bin/perl"), sfw},
		shebangSig{[]byte("/usr/bin/env perl"), scwsfw},
		shebangSig{[]byte("/usr/bin/env -S perl"), scwsfw},
	)
}

// Python matches a Python programming language file.
func Python(raw []byte, _ uint32) bool {
	return shebang(raw,
		shebangSig{[]byte("/usr/bin/python"), snone},
		shebangSig{[]byte("/usr/local/bin/python"), snone},
		shebangSig{[]byte("/usr/bin/env python"), scws},
		shebangSig{[]byte("/usr/bin/env -S python"), scws},
		shebangSig{[]byte("/usr/bin/python2"), snone},
		shebangSig{[]byte("/usr/local/bin/python2"), snone},
		shebangSig{[]byte("/usr/bin/env python2"), scws},
		shebangSig{[]byte("/usr/bin/env -S python2"), scws},
		shebangSig{[]byte("/usr/bin/python3"), snone},
		shebangSig{[]byte("/usr/local/bin/python3"), snone},
		shebangSig{[]byte("/usr/bin/env python3"), scws},
		shebangSig{[]byte("/usr/bin/env -S python3"), scws},
	)

}

// Ruby matches a Ruby programming language file.
func Ruby(raw []byte, _ uint32) bool {
	return shebang(raw,
		shebangSig{[]byte("/usr/bin/ruby"), snone},
		shebangSig{[]byte("/usr/local/bin/ruby"), snone},
		shebangSig{[]byte("/usr/bin/env ruby"), scws},
		shebangSig{[]byte("/usr/bin/env -S ruby"), scws},
	)
}

// Tcl matches a Tcl programming language file.
func Tcl(raw []byte, _ uint32) bool {
	return shebang(raw,
		shebangSig{[]byte("/usr/bin/tcl"), snone},
		shebangSig{[]byte("/usr/local/bin/tcl"), snone},
		shebangSig{[]byte("/usr/bin/env tcl"), scws},
		shebangSig{[]byte("/usr/bin/env -S tcl"), scws},
		shebangSig{[]byte("/usr/bin/tclsh"), snone},
		shebangSig{[]byte("/usr/local/bin/tclsh"), snone},
		shebangSig{[]byte("/usr/bin/env tclsh"), scws},
		shebangSig{[]byte("/usr/bin/env -S tclsh"), scws},
		shebangSig{[]byte("/usr/bin/wish"), snone},
		shebangSig{[]byte("/usr/local/bin/wish"), snone},
		shebangSig{[]byte("/usr/bin/env wish"), scws},
		shebangSig{[]byte("/usr/bin/env -S wish"), scws},
	)
}

// Rtf matches a Rich Text Format file.
func Rtf(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte("{\\rtf"))
}

// Shell matches a shell script file.
func Shell(raw []byte, _ uint32) bool {
	return shebang(raw,
		shebangSig{[]byte("/bin/sh"), sfw},
		shebangSig{[]byte("/bin/bash"), sfw},
		shebangSig{[]byte("/usr/local/bin/bash"), sfw},
		shebangSig{[]byte("/usr/bin/env bash"), scwsfw},
		shebangSig{[]byte("/usr/bin/env -S bash"), scwsfw},
		shebangSig{[]byte("/bin/csh"), sfw},
		shebangSig{[]byte("/usr/local/bin/csh"), sfw},
		shebangSig{[]byte("/usr/bin/env csh"), scwsfw},
		shebangSig{[]byte("/usr/bin/env -S csh"), scwsfw},
		shebangSig{[]byte("/bin/dash"), sfw},
		shebangSig{[]byte("/usr/local/bin/dash"), sfw},
		shebangSig{[]byte("/usr/bin/env dash"), scwsfw},
		shebangSig{[]byte("/usr/bin/env -S dash"), scwsfw},
		shebangSig{[]byte("/bin/ksh"), sfw},
		shebangSig{[]byte("/usr/local/bin/ksh"), sfw},
		shebangSig{[]byte("/usr/bin/env ksh"), scwsfw},
		shebangSig{[]byte("/usr/bin/env -S ksh"), scwsfw},
		shebangSig{[]byte("/bin/tcsh"), sfw},
		shebangSig{[]byte("/usr/local/bin/tcsh"), sfw},
		shebangSig{[]byte("/usr/bin/env tcsh"), scwsfw},
		shebangSig{[]byte("/usr/bin/env -S tcsh"), scwsfw},
		shebangSig{[]byte("/bin/zsh"), sfw},
		shebangSig{[]byte("/usr/local/bin/zsh"), sfw},
		shebangSig{[]byte("/usr/bin/env zsh"), scwsfw},
		shebangSig{[]byte("/usr/bin/env -S zsh"), scwsfw},
	)
}

// Text matches a plain text file.
//
// TODO: This function does not parse BOM-less UTF16 and UTF32 files. Not really
// sure it should. libmagic also requires a BOM for UTF16 and UTF32.
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

// CDXJSON matches a CycloneDX JSON BOM file.
// https://cyclonedx.org/docs/1.7/json/
func CDXJSON(raw []byte, limit uint32) bool {
	return jsonHelper(raw, limit, json.QueryCDX, json.TokObject)
}

// jsonHelper parses raw and tries to match the q query against it. wantToks
// ensures we're not wasting time parsing an input that would not pass anyway,
// ex: the input is a valid JSON array, but we're looking for a JSON object.
func jsonHelper(raw scan.Bytes, limit uint32, q string, wantToks ...int) bool {
	firstNonWS := raw.FirstNonWS()

	hasTargetTok := false
	for _, t := range wantToks {
		hasTargetTok = hasTargetTok || (t&json.TokArray > 0 && firstNonWS == '[')
		hasTargetTok = hasTargetTok || (t&json.TokObject > 0 && firstNonWS == '{')
	}
	if !hasTargetTok {
		return false
	}
	lraw := len(raw)
	parsed, inspected, _, querySatisfied := json.Parse(q, raw)
	if !querySatisfied {
		return false
	}

	// If the full file content was provided, check that the whole input was parsed.
	if limit == 0 || lraw < int(limit) {
		return parsed == lraw
	}

	// If a section of the file was provided, check if all of it was inspected.
	// In other words, check that if there was a problem parsing, that problem
	// occurred after the last byte in the input.
	return inspected == lraw && lraw > 0
}

// NdJSON matches a Newline delimited JSON file. All complete lines from raw
// must be valid JSON documents meaning they contain one of the valid JSON data
// types.
func NdJSON(raw []byte, limit uint32) bool {
	lCount, objOrArr := 0, 0

	s := scan.Bytes(raw)
	var l scan.Bytes
	for len(s) != 0 {
		l = s.Line()
		parsed, inspected, firstToken, _ := json.Parse(json.QueryNone, l)
		// Only the last line may be truncated by the read limit; for it, it is
		// enough that the parser inspected all of it. Every other line must be a
		// complete, valid JSON document, otherwise a single JSON document spread
		// over multiple lines would be mistaken for NDJSON. #803
		if len(s) == 0 {
			if inspected != len(l) {
				return false
			}
		} else if parsed != len(l) {
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

type rfc822Hint struct {
	h          []byte
	matchFlags scan.Flags
}

// The hints come from libmagic, but the implementation is bit different. libmagic
// only checks if the file starts with the hint, while we additionally look for
// a secondary hint in the first few lines of input.
func RFC822(raw []byte, limit uint32) bool {
	b := scan.Bytes(raw)

	// Keep hints here to avoid instantiating them several times in lineHasRFC822Hint.
	// The alternative is to make them a package level var, but then they'd go
	// on the heap.
	// Some of the hints are IgnoreCase, some not. I selected based on what libmagic
	// does and based on personal observations from sample files.
	hints := []rfc822Hint{
		// Enron dataset has Message-ID, Message-Id and Message-id.
		{[]byte("Message-ID: "), scan.IgnoreCase},
		{[]byte("From: "), 0},
		{[]byte("To: "), 0},
		{[]byte("CC: "), scan.IgnoreCase},
		{[]byte("Date: "), 0},
		{[]byte("Subject: "), 0},
		{[]byte("Received: "), 0},
		{[]byte("Relay-Version: "), 0},
		{[]byte("#! rnews"), 0},
		{[]byte("N#! rnews"), 0},
		{[]byte("Forward to"), 0},
		{[]byte("Pipe to"), 0},
		{[]byte("DELIVERED-TO: "), scan.IgnoreCase},
		{[]byte("RETURN-PATH: "), scan.IgnoreCase},
		{[]byte("Content-Type: "), 0},
		{[]byte("Content-Transfer-Encoding: "), 0},
	}
	if !lineHasRFC822Hint(b.Line(), hints) {
		return false
	}
	for i := 0; i < 20; i++ {
		if lineHasRFC822Hint(b.Line(), hints) {
			return true
		}
	}

	return false
}

func lineHasRFC822Hint(b scan.Bytes, hints []rfc822Hint) bool {
	for _, h := range hints {
		if b.Match(h.h, h.matchFlags) > -1 {
			return true
		}
	}
	return false
}

func GEDCOM(raw []byte, limit uint32) bool {
	// Skip if empty
	if len(raw) == 0 {
		return false
	}

	// GEDCOM header fits within first 4KB
	searchLimit := min(len(raw), 4096)
	raw = raw[:searchLimit]

	b := scan.Bytes(raw)

	// Skip BOM if present: UTF-8, UTF-16BE, UTF-16LE
	for _, bom := range [][]byte{
		{0xEF, 0xBB, 0xBF}, // UTF-8
		{0xFE, 0xFF},       // UTF-16BE
		{0xFF, 0xFE},       // UTF-16LE
	} {
		if bytes.HasPrefix(b, bom) {
			b.Advance(len(bom))
			break // Only one BOM can exist at the start
		}
	}

	b.TrimLWS()

	firstLine := b.Line()
	if !bytes.Equal(firstLine, []byte("0 HEAD")) {
		return false
	}

	// "1 GEDC" is mandatory in the header
	for i := 0; i < 10; i++ {
		line := b.Line()
		if bytes.Equal(line, []byte("1 GEDC")) {
			return true
		}
	}
	return false
}

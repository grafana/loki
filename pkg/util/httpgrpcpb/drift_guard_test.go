package httpgrpcpb_test

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/grafana/dskit/httpgrpc"

	"github.com/grafana/loki/v3/pkg/util/httpgrpcpb"
)

// This file guards pkg/util/httpgrpcpb/httpgrpcpb.proto, a hand-maintained local shadow
// of dskit's httpgrpc.proto that wiresmith-migrated protos import instead of the vendored
// gogo original, because wiresmith-generated code cannot embed message types from another
// protobuf runtime, and customtype is rejected on oneof variants -- HTTPRequest/
// HTTPResponse are oneof variants in frontendv1pb/schedulerpb (see this package's own doc
// comment, and WIRESMITH_MIGRATION.md). The shadow has no compiler-enforced link back to
// the vendored source, so nothing else catches httpgrpc.proto changing shape out from
// under it. Three checks guard against that:
//
//   - TestHTTPGRPCShadowMatchesVendoredSchema normalizes and diffs the two .proto files'
//     tracked message declarations, keyed by field number (so declaration-order-only
//     changes don't false-positive), and fails closed on any field it doesn't recognize
//     as a plain scalar/message field (oneofs, maps, enums, nested messages, reserved
//     ranges) rather than silently ignoring it.
//   - TestHTTPGRPCWireRoundTrip proves that, as the two schemas stand today, marshaling
//     with one runtime and unmarshaling with the other reproduces the original message in
//     both directions -- catching a shape mismatch the schema-diff might miss (e.g. a
//     still name/number/type-compatible field whose conversion was never wired up).
//   - TestHTTPGRPCConvertHelpersRoundTrip regression-tests convert.go's
//     FromHTTPRequest/ToHTTPRequest/FromHTTPResponse/ToHTTPResponse, the call-site glue
//     pkg/lokifrontend and pkg/scheduler actually use.
//
// If any of these fail, the fix is:
//
//  1. Diff vendor/github.com/grafana/dskit/httpgrpc/httpgrpc.proto against
//     pkg/util/httpgrpcpb/httpgrpcpb.proto to see what changed.
//  2. Re-sync the shadow's HTTPRequest/HTTPResponse/Header fields to match, keeping the
//     wiresmith.options annotations (pointer=true on repeated Header fields,
//     no_presence_all, etc.) in the same style as the surrounding fields.
//  3. Regenerate with the pinned wiresmith compiler (`make wiresmith-protos`, or
//     `make protos`).
//  4. Update pkg/util/httpgrpcpb/convert.go if the field set changed, then re-run these
//     tests.
//
// See also pkg/engine/internal/proto/wirepb/drift_guard_test.go, which guards the second,
// separate shadow of dskit's Header message that wirepb.proto carries for its
// HeaderAdapter customtype bridge.

const dskitHTTPGRPCProtoPath = "../../../vendor/github.com/grafana/dskit/httpgrpc/httpgrpc.proto"

// trackedMessages lists the messages httpgrpcpb.proto's own doc comment identifies as
// wire-identical copies of the corresponding dskit messages. Nothing else in either file
// is compared: httpgrpcpb.proto only ever needs to shadow the messages actually
// referenced from wiresmith-migrated protos.
var trackedMessages = []string{"HTTPRequest", "HTTPResponse", "Header"}

// protoField is one field's wire-relevant shape as read from a .proto source file: type,
// name, and repeated-ness. Field number is deliberately not part of this struct -- it's
// the map key in extractMessageFields, since proto3 wire compatibility is governed by
// field number, not declaration order.
type protoField struct {
	Type     string
	Name     string
	Repeated bool
}

// protoFieldRe matches a single scalar/message field declaration, e.g.:
//
//	string method = 1;
//	repeated Header headers = 3 [(wiresmith.options.pointer) = true];
//
// The trailing bracketed field options are matched but discarded: they select
// per-runtime codegen behavior (wiresmith vs. gogoproto annotations), not wire shape.
var protoFieldRe = regexp.MustCompile(`^(repeated\s+)?([A-Za-z_][A-Za-z0-9_.]*)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(\d+)\s*(\[[^\]]*\])?;$`)

// messageOpenRe returns a regexp matching the top-level declaration line of
// "message <name> {", anchored to the start of a line so a mention of the name inside a
// comment can't be mistaken for the real declaration.
func messageOpenRe(name string) *regexp.Regexp {
	return regexp.MustCompile(`(?m)^message\s+` + regexp.QuoteMeta(name) + `\s*\{`)
}

// extractMessageFields locates the first top-level "message <messageName> { ... }" block
// in src and parses its field declarations into a map keyed by field number. It fails the
// test (via require, so callers don't need to check a returned error) if the message
// can't be found, its braces don't balance, or any line inside its body isn't a
// recognized scalar/message field declaration: this guard's whole job is to notice shape
// drift, so silently skipping a construct it doesn't understand (oneofs, maps, enums,
// nested messages, reserved ranges) would defeat the purpose.
func extractMessageFields(t *testing.T, src, messageName string) map[int]protoField {
	t.Helper()

	loc := messageOpenRe(messageName).FindStringIndex(src)
	require.NotNilf(t, loc, "no top-level message %q found in proto source", messageName)
	bodyStart := loc[1]

	depth := 1
	end := -1
	for i := bodyStart; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				end = i
			}
		}
		if end != -1 {
			break
		}
	}
	require.NotEqualf(t, -1, end, "unbalanced braces while scanning message %q", messageName)

	fields := map[int]protoField{}
	for i, raw := range strings.Split(src[bodyStart:end], "\n") {
		line := raw
		if idx := strings.Index(line, "//"); idx != -1 {
			line = line[:idx]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		m := protoFieldRe.FindStringSubmatch(line)
		require.NotNilf(t, m,
			"message %s, line %d: %q is not a recognized scalar/message field declaration. "+
				"This drift guard only understands flat fields; oneofs, maps, enums, nested "+
				"messages, and reserved statements need the guard updated -- and its author "+
				"confirming the construct's wire-compatibility story -- before it can be "+
				"trusted again", messageName, i+1, strings.TrimSpace(raw))

		num, err := strconv.Atoi(m[4])
		require.NoErrorf(t, err, "message %s: invalid field number %q", messageName, m[4])
		_, dup := fields[num]
		require.Falsef(t, dup, "message %s: duplicate field number %d", messageName, num)

		fields[num] = protoField{Type: m[2], Name: m[3], Repeated: m[1] != ""}
	}
	return fields
}

func readProtoFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoErrorf(t, err, "reading %s (relative paths assume `go test` runs from the package directory)", path)
	return string(data)
}

const regenInstructions = "Re-sync the field list in pkg/util/httpgrpcpb/httpgrpcpb.proto from " +
	"vendor/github.com/grafana/dskit/httpgrpc/httpgrpc.proto, regenerate with `make wiresmith-protos` " +
	"(or `make protos`), and reconcile pkg/util/httpgrpcpb/convert.go and its call sites with any shape change."

// TestHTTPGRPCShadowMatchesVendoredSchema fails if pkg/util/httpgrpcpb/httpgrpcpb.proto's
// HTTPRequest/HTTPResponse/Header messages no longer match the field shape (name, number,
// type, repeated-ness) of the vendored dskit httpgrpc.proto, modulo wiresmith option
// annotations.
func TestHTTPGRPCShadowMatchesVendoredSchema(t *testing.T) {
	dskitSrc := readProtoFile(t, dskitHTTPGRPCProtoPath)
	shadowSrc := readProtoFile(t, "httpgrpcpb.proto")

	for _, msg := range trackedMessages {
		t.Run(msg, func(t *testing.T) {
			want := extractMessageFields(t, dskitSrc, msg)
			got := extractMessageFields(t, shadowSrc, msg)
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("httpgrpcpb.proto's %s has drifted from vendored dskit httpgrpc.proto's %s, "+
					"keyed by field number (-vendored dskit, +local shadow):\n%s\n%s", msg, msg, diff, regenInstructions)
			}
		})
	}
}

// sampleDskitRequest, sampleWiresmithRequest, and the HTTPResponse pair below build
// field-for-field equivalent values in both runtimes' Go types, with every field
// populated, including a multi-entry Headers slice (itself multi-value per entry) and
// non-empty Body bytes. Shared by TestHTTPGRPCWireRoundTrip and
// TestHTTPGRPCConvertHelpersRoundTrip below.

func sampleDskitRequest() *httpgrpc.HTTPRequest {
	return &httpgrpc.HTTPRequest{
		Method: "POST",
		Url:    "/loki/api/v1/push?tenant=fake",
		Headers: []*httpgrpc.Header{
			{Key: "Content-Type", Values: []string{"application/json"}},
			{Key: "X-Scope-OrgID", Values: []string{"fake", "fake-2"}},
		},
		Body: []byte(`{"streams":[]}`),
	}
}

func sampleWiresmithRequest() *httpgrpcpb.HTTPRequest {
	return &httpgrpcpb.HTTPRequest{
		Method: "POST",
		Url:    "/loki/api/v1/push?tenant=fake",
		Headers: []*httpgrpcpb.Header{
			{Key: "Content-Type", Values: []string{"application/json"}},
			{Key: "X-Scope-OrgID", Values: []string{"fake", "fake-2"}},
		},
		Body: []byte(`{"streams":[]}`),
	}
}

func sampleDskitResponse() *httpgrpc.HTTPResponse {
	return &httpgrpc.HTTPResponse{
		Code: 200,
		Headers: []*httpgrpc.Header{
			{Key: "Content-Type", Values: []string{"application/json"}},
		},
		Body: []byte(`{"status":"success"}`),
	}
}

func sampleWiresmithResponse() *httpgrpcpb.HTTPResponse {
	return &httpgrpcpb.HTTPResponse{
		Code: 200,
		Headers: []*httpgrpcpb.Header{
			{Key: "Content-Type", Values: []string{"application/json"}},
		},
		Body: []byte(`{"status":"success"}`),
	}
}

// TestHTTPGRPCWireRoundTrip proves that, as httpgrpcpb.proto and the vendored dskit
// httpgrpc.proto stand today, the two independently-generated codecs actually produce and
// consume identical bytes on the wire, in both directions. This does not by itself catch
// every possible drift (a newly added upstream field has no shadow counterpart to diff
// against, and would just marshal/unmarshal as an unrecognized/dropped field on either
// side) -- that's what TestHTTPGRPCShadowMatchesVendoredSchema is for. This test instead
// guards against the two schemas agreeing on paper while actually encoding or decoding
// differently.
func TestHTTPGRPCWireRoundTrip(t *testing.T) {
	t.Run("HTTPRequest gogo-marshal wiresmith-unmarshal", func(t *testing.T) {
		b, err := sampleDskitRequest().Marshal()
		require.NoError(t, err)

		got := &httpgrpcpb.HTTPRequest{}
		require.NoError(t, got.Unmarshal(b))

		if diff := cmp.Diff(sampleWiresmithRequest(), got); diff != "" {
			t.Errorf("HTTPRequest marshaled by the vendored dskit gogo runtime does not unmarshal "+
				"identically via the wiresmith pkg/util/httpgrpcpb shadow (-want, +got):\n%s\n%s", diff, regenInstructions)
		}
	})

	t.Run("HTTPRequest wiresmith-marshal gogo-unmarshal", func(t *testing.T) {
		b, err := sampleWiresmithRequest().Marshal()
		require.NoError(t, err)

		got := &httpgrpc.HTTPRequest{}
		require.NoError(t, got.Unmarshal(b))

		if diff := cmp.Diff(sampleDskitRequest(), got); diff != "" {
			t.Errorf("HTTPRequest marshaled by the wiresmith pkg/util/httpgrpcpb shadow does not unmarshal "+
				"identically via the vendored dskit gogo runtime (-want, +got):\n%s\n%s", diff, regenInstructions)
		}
	})

	t.Run("HTTPResponse gogo-marshal wiresmith-unmarshal", func(t *testing.T) {
		b, err := sampleDskitResponse().Marshal()
		require.NoError(t, err)

		got := &httpgrpcpb.HTTPResponse{}
		require.NoError(t, got.Unmarshal(b))

		if diff := cmp.Diff(sampleWiresmithResponse(), got); diff != "" {
			t.Errorf("HTTPResponse marshaled by the vendored dskit gogo runtime does not unmarshal "+
				"identically via the wiresmith pkg/util/httpgrpcpb shadow (-want, +got):\n%s\n%s", diff, regenInstructions)
		}
	})

	t.Run("HTTPResponse wiresmith-marshal gogo-unmarshal", func(t *testing.T) {
		b, err := sampleWiresmithResponse().Marshal()
		require.NoError(t, err)

		got := &httpgrpc.HTTPResponse{}
		require.NoError(t, got.Unmarshal(b))

		if diff := cmp.Diff(sampleDskitResponse(), got); diff != "" {
			t.Errorf("HTTPResponse marshaled by the wiresmith pkg/util/httpgrpcpb shadow does not unmarshal "+
				"identically via the vendored dskit gogo runtime (-want, +got):\n%s\n%s", diff, regenInstructions)
		}
	})
}

// TestHTTPGRPCConvertHelpersRoundTrip exercises convert.go's
// FromHTTPRequest/ToHTTPRequest/FromHTTPResponse/ToHTTPResponse, the call-site helpers
// pkg/lokifrontend and pkg/scheduler use to bridge between the two types, including nil
// handling.
func TestHTTPGRPCConvertHelpersRoundTrip(t *testing.T) {
	t.Run("HTTPRequest", func(t *testing.T) {
		original := sampleDskitRequest()

		local := httpgrpcpb.FromHTTPRequest(original)
		require.Equal(t, sampleWiresmithRequest(), local)

		back := httpgrpcpb.ToHTTPRequest(local)
		require.Equal(t, original, back)
	})

	t.Run("HTTPResponse", func(t *testing.T) {
		original := sampleDskitResponse()

		local := httpgrpcpb.FromHTTPResponse(original)
		require.Equal(t, sampleWiresmithResponse(), local)

		back := httpgrpcpb.ToHTTPResponse(local)
		require.Equal(t, original, back)
	})

	t.Run("nil", func(t *testing.T) {
		require.Nil(t, httpgrpcpb.FromHTTPRequest(nil))
		require.Nil(t, httpgrpcpb.ToHTTPRequest(nil))
		require.Nil(t, httpgrpcpb.FromHTTPResponse(nil))
		require.Nil(t, httpgrpcpb.ToHTTPResponse(nil))
	})
}

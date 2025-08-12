package dataobj

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/scratch"
)

func Test_encoder_typeRefs(t *testing.T) {
	tt := []struct {
		name  string
		input SectionType

		expectRef     uint32
		expectNameRef *filemd.SectionType_NameRef
	}{
		{
			name:  "invalid",
			input: SectionType{},

			expectRef:     0,
			expectNameRef: nil,
		},
		{
			name:  "streams",
			input: SectionType{"github.com/grafana/loki", "streams"},

			expectRef:     1,
			expectNameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 2},
		},
		{
			name:  "logs",
			input: SectionType{"github.com/grafana/loki", "logs"},

			expectRef:     2,
			expectNameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 3},
		},
		{
			name: "existing namespace, new kind",
			input: SectionType{
				Namespace: "github.com/grafana/loki",
				Kind:      "section-kind-1",
			},

			expectRef:     3,
			expectNameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 4},
		},
		{
			name: "existing namespace, existing kind",
			input: SectionType{
				Namespace: "github.com/grafana/loki",
				Kind:      "section-kind-1",
			},

			expectRef:     3,
			expectNameRef: &filemd.SectionType_NameRef{NamespaceRef: 1, KindRef: 4},
		},
		{
			name: "new namespace, existing kind",
			input: SectionType{
				Namespace: "new-namespace",
				Kind:      "section-kind-1",
			},

			expectRef:     4,
			expectNameRef: &filemd.SectionType_NameRef{NamespaceRef: 5, KindRef: 4},
		},
		{
			name: "new namespace, new kind",
			input: SectionType{
				Namespace: "new-namespace-2",
				Kind:      "section-kind-2",
			},

			expectRef:     5,
			expectNameRef: &filemd.SectionType_NameRef{NamespaceRef: 6, KindRef: 7},
		},
	}

	enc := newEncoder(scratch.NewMemory())

	// Test are run sequentially so we can check the behaviour of streaming types
	// in.
	for _, tc := range tt {
		typeRef := enc.getTypeRef(tc.input)
		nameRef := enc.rawTypes[typeRef].NameRef

		assert.Equal(t, tc.expectRef, typeRef, "unexpected type ref for %s", tc.name)
		assert.Equal(t, tc.expectNameRef, nameRef, "unexpected name ref for %s", tc.name)
	}

}

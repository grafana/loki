package encoding

import (
	"errors"
	"fmt"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
)

var (
	ErrInvalidSectionType = errors.New("invalid section type")
	ErrMissingSectionType = errors.New("missing section type")
)

// SectionType is a type of section in a data object. The zero value of
// SectionType represents an invalid type.
type SectionType struct {
	Namespace string // Namespace of the type.
	Kind      string // Kind of the type.
}

// Valid returns whether the SectionType is valid. Both the namespace and kind
// fields must be set for the type to be valid.
func (typ SectionType) Valid() bool {
	return typ.Namespace != "" && typ.Kind != ""
}

func (typ SectionType) String() string {
	if !typ.Valid() {
		return "<invalid>"
	}
	return fmt.Sprintf("%s/%s", typ.Namespace, typ.Kind)
}

var (
	SectionTypeInvalid = SectionType{}
	SectionTypeStreams = SectionType{"github.com/grafana/loki", "streams"}
	SectionTypeLogs    = SectionType{"github.com/grafana/loki", "logs"}

	legacyKindMapping = map[filemd.SectionKind]SectionType{ //nolint:staticcheck // Ignore deprecation warning
		filemd.SECTION_KIND_UNSPECIFIED: SectionTypeInvalid,
		filemd.SECTION_KIND_STREAMS:     SectionTypeStreams,
		filemd.SECTION_KIND_LOGS:        SectionTypeLogs,
	}
)

// GetSectionType returns the [SectionType] for the given section.
// GetSectionType is backwards compatible with the old section kind system. The
// md argument is used to look up the type in the metadata.
//
// GetSectionType returns an error if the section incorrectly specifies both
// the legacy kind and the new type reference.
func GetSectionType(md *filemd.Metadata, section *filemd.SectionInfo) (SectionType, error) {
	var (
		deprecatedKind = section.Kind //nolint:staticcheck // Ignore deprecation warning
	)

	switch {
	case deprecatedKind != filemd.SECTION_KIND_UNSPECIFIED && section.TypeRef != 0:
		return SectionTypeInvalid, errors.New("section specifies both legacy kind and new type reference")

	case deprecatedKind != filemd.SECTION_KIND_UNSPECIFIED:
		typ, ok := legacyKindMapping[deprecatedKind]
		if !ok {
			return SectionTypeInvalid, ErrInvalidSectionType
		}
		return typ, nil

	case section.TypeRef != 0:
		if section.TypeRef >= uint32(len(md.Types)) {
			return SectionTypeInvalid, fmt.Errorf("%w: typeRef %d out of bounds [1, %d)", ErrMissingSectionType, section.TypeRef, len(md.Types))
		}

		var (
			rawType = md.Types[section.TypeRef]

			namespaceRef = rawType.NameRef.NamespaceRef
			kindRef      = rawType.NameRef.KindRef
		)

		// Validate the namespace and kind references.
		if namespaceRef == 0 || namespaceRef >= uint32(len(md.Dictionary)) {
			return SectionTypeInvalid, fmt.Errorf("%w: namespaceRef %d out of bounds [1, %d)", ErrMissingSectionType, namespaceRef, len(md.Dictionary))
		} else if kindRef == 0 || kindRef >= uint32(len(md.Dictionary)) {
			return SectionTypeInvalid, fmt.Errorf("%w: kindRef %d out of bounds [1, %d)", ErrMissingSectionType, kindRef, len(md.Dictionary))
		}

		return SectionType{
			Namespace: md.Dictionary[namespaceRef],
			Kind:      md.Dictionary[kindRef],
		}, nil
	}

	return SectionTypeInvalid, ErrMissingSectionType
}

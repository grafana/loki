package parquet

import (
	"fmt"

	"github.com/parquet-go/parquet-go/variant"
)

// This file teaches schema conversion to read shredded variant columns.
// When the target schema declares an unshredded variant group (metadata,
// value) and the source schema stores the column shredded (metadata, value,
// typed_value), the conversion reconstructs each value with the
// construct_variant algorithm (see variant_shredded_read.go) and emits the
// re-encoded variant binary. Without this, the generic column mapping would
// silently drop the typed_value columns, losing every shredded value.
//
// Because every read path funnels through Convert when the reader and file
// schemas differ, this single integration point makes shredded files
// readable by any reader that declares a plain variant column.

// variantConversion reconstructs one shredded variant column during row
// conversion.
type variantConversion struct {
	group *shreddedVariantGroup
	// A source group that cannot be reconstructed (e.g. an unsupported
	// shredded type) fails when rows are read, not when the conversion is
	// constructed, matching how readers report other malformed files.
	err         error
	numCols     int // leaf columns of the source variant group
	sourceStart int // first source leaf column of the variant group

	targetMetaCol  int
	targetValueCol int
	targetDef      byte // definition level of the target leaves when present

	sourceGroupDef int // definition level at which the source group is present
	sourceGroupRep int // repetition level of the source group

	// Source-to-target level mappings for the path down to the variant
	// group, used for occurrences where the group is null because of a
	// null enclosing level.
	defLevels []byte
	repLevels []byte
}

// findVariantConversions walks the target and source schemas in parallel
// and returns a conversion for every variant column that the target
// declares unshredded and the source stores shredded.
func findVariantConversions(to, from Node) []variantConversion {
	var conversions []variantConversion
	walkVariantConversions(to, from, 0, 0, 0, 0, 0, 0, []byte{0}, []byte{0}, &conversions)
	return conversions
}

func walkVariantConversions(t, s Node, tCol, sCol int, tRep, tDef, sRep, sDef byte, repLevels, defLevels []byte, conversions *[]variantConversion) {
	if isVariant(t) && isVariant(s) && isUnshreddedVariant(t) && !isUnshreddedVariant(s) {
		if vc := makeVariantConversion(t, s, tCol, sCol, tDef, sDef, sRep, repLevels, defLevels); vc != nil {
			*conversions = append(*conversions, *vc)
		}
		return
	}
	if t.Leaf() || s.Leaf() {
		return
	}
	sColOf := make(map[string]int, len(s.Fields()))
	sFieldOf := make(map[string]Field, len(s.Fields()))
	col := sCol
	for _, sf := range s.Fields() {
		sColOf[sf.Name()] = col
		sFieldOf[sf.Name()] = sf
		col += int(numLeafColumnsOf(sf))
	}
	for _, tf := range t.Fields() {
		if sf, ok := sFieldOf[tf.Name()]; ok {
			tr, td := applyFieldRepetitionType(fieldRepetitionTypeOf(tf), tRep, tDef)
			sr, sd := applyFieldRepetitionType(fieldRepetitionTypeOf(sf), sRep, sDef)
			walkVariantConversions(
				tf, sf,
				tCol, sColOf[tf.Name()],
				tr, td, sr, sd,
				setLevelMapping(repLevels, sr, tr),
				setLevelMapping(defLevels, sd, td),
				conversions,
			)
		}
		tCol += int(numLeafColumnsOf(tf))
	}
}

// makeVariantConversion builds the conversion for one variant column. The
// level arguments are those of the variant group nodes themselves. A nil
// result means the target group does not have the expected metadata and
// value leaves and the generic column mapping applies.
func makeVariantConversion(t, s Node, tCol, sCol int, tDef, sDef, sRep byte, repLevels, defLevels []byte) *variantConversion {
	vc := &variantConversion{
		numCols:        int(numLeafColumnsOf(s)),
		sourceStart:    sCol,
		targetMetaCol:  -1,
		targetValueCol: -1,
		targetDef:      tDef,
		sourceGroupDef: int(sDef),
		sourceGroupRep: int(sRep),
		defLevels:      defLevels,
		repLevels:      repLevels,
	}
	col := tCol
	for _, tf := range t.Fields() {
		if !tf.Leaf() || tf.Type().Kind() != ByteArray || tf.Optional() {
			return nil
		}
		switch tf.Name() {
		case "metadata":
			vc.targetMetaCol = col
		case "value":
			vc.targetValueCol = col
		}
		col++
	}
	if vc.targetMetaCol < 0 || vc.targetValueCol < 0 {
		return nil
	}

	vc.group, vc.err = buildShreddedVariantGroup(s, 0, 0, 0)
	if vc.err == nil && vc.group.metadataCol < 0 {
		vc.group, vc.err = nil, fmt.Errorf("variant: shredded variant group has no metadata field")
	}
	return vc
}

// setLevelMapping returns a copy of the source-to-target level mapping with
// the entry for the source level set, extending the table when the source
// level is new (each schema step adds at most one level).
func setLevelMapping(levels []byte, source, target byte) []byte {
	mapping := make([]byte, max(len(levels), int(source)+1))
	copy(mapping, levels)
	mapping[source] = target
	return mapping
}

// variantScratch is the per-row state of one variant conversion, pooled in
// conversionBuffer so converting a row does not allocate it anew.
type variantScratch struct {
	pos    []int
	metas  []Value
	values []Value
}

// convert reconstructs every occurrence of the variant column in the
// current row from the source columns, leaving the values of the target
// metadata and value columns in the scratch space.
func (vc *variantConversion) convert(source [][]Value, scratch *variantScratch) error {
	if vc.err != nil {
		return vc.err
	}
	columns := source[vc.sourceStart : vc.sourceStart+vc.numCols]
	if cap(scratch.pos) < len(columns) {
		scratch.pos = make([]int, len(columns))
	}
	scratch.pos = scratch.pos[:len(columns)]
	clear(scratch.pos)
	scratch.metas = scratch.metas[:0]
	scratch.values = scratch.values[:0]
	reader := variantColumnReader{columns: columns, pos: scratch.pos}
	metaIdx := ^uint16(vc.targetMetaCol)
	valueIdx := ^uint16(vc.targetValueCol)

	for {
		metaVal, ok := reader.peek(vc.group.metadataCol)
		if !ok {
			break
		}

		// The level mapping tables cover the levels the schema can
		// produce; corrupt files can carry level bytes beyond them, which
		// must surface as a decode error rather than a panic.
		if int(metaVal.repetitionLevel) >= len(vc.repLevels) || int(metaVal.definitionLevel) >= len(vc.defLevels) {
			return fmt.Errorf("variant: metadata column has levels (r=%d, d=%d) outside the schema's levels",
				metaVal.repetitionLevel, metaVal.definitionLevel)
		}

		if int(metaVal.definitionLevel) < vc.sourceGroupDef {
			// The variant group is null at this occurrence; every leaf
			// column of the group carries exactly one value at the
			// enclosing level to consume.
			rep := vc.repLevels[metaVal.repetitionLevel]
			def := vc.defLevels[metaVal.definitionLevel]
			scratch.metas = append(scratch.metas, Value{repetitionLevel: rep, definitionLevel: def, columnIndex: metaIdx})
			scratch.values = append(scratch.values, Value{repetitionLevel: rep, definitionLevel: def, columnIndex: valueIdx})
			for col := range columns {
				reader.next(col)
			}
			continue
		}

		reader.next(vc.group.metadataCol)
		m, err := variant.DecodeMetadata(metaVal.ByteArray())
		if err != nil {
			return fmt.Errorf("variant metadata: %w", err)
		}
		v, present, err := vc.group.read(&reader, m, vc.sourceGroupDef, vc.sourceGroupRep)
		if err != nil {
			return err
		}
		if !present {
			// value and typed_value both null at the top level is invalid
			// per the spec; read it as variant null (matching parquet-java).
			v = variant.Null()
		}

		var b variant.MetadataBuilder
		encoded := variant.Encode(&b, v)
		_, metadata := b.Build()
		rep := vc.repLevels[metaVal.repetitionLevel]

		metaValue := ByteArrayValue(metadata)
		metaValue.repetitionLevel = rep
		metaValue.definitionLevel = vc.targetDef
		metaValue.columnIndex = metaIdx

		valueValue := ByteArrayValue(encoded)
		valueValue.repetitionLevel = rep
		valueValue.definitionLevel = vc.targetDef
		valueValue.columnIndex = valueIdx

		scratch.metas = append(scratch.metas, metaValue)
		scratch.values = append(scratch.values, valueValue)
	}
	if len(scratch.metas) == 0 {
		// No values in the source columns for this row; emit a null pair
		// to maintain the one-value-per-column invariant.
		scratch.metas = append(scratch.metas, Value{columnIndex: metaIdx})
		scratch.values = append(scratch.values, Value{columnIndex: valueIdx})
	}
	return nil
}

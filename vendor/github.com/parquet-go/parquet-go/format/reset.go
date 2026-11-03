package format

// The Reset methods in this file clear values in place while retaining
// allocated slice capacity, so that a value can be reused as the decode
// target of multiple thrift deserializations without allocating on each
// decode (see FooterDecoder).
//
// Because the thrift decoder only writes fields that are present in the
// input, Reset must recursively clear the elements of retained slices;
// otherwise fields decoded from a previous input would leak into the next
// decoded value. Union fields are the exception: the decoder itself clears
// unions absent from the input and zeroes reused members, so Reset retains
// their member allocations for the next decode to reuse.
//
// Byte slice and string fields decoded from footers alias the input buffer
// (they are replaced, never reused, by the decoder), so Reset sets them to
// nil or empty instead of truncating, releasing references to the previous
// input buffer. This includes the contents of retained union members whose
// fields alias the buffer: the member allocation is kept for the next
// decode to reuse, but its contents are zeroed so no retained member holds
// a reference into a buffer with unrelated data.

// Reset clears m in place, retaining allocated capacity for reuse in
// subsequent thrift decodes.
//
// Note that after reuse, optional list fields that are absent from the
// decoded input are left empty but non-nil, whereas decoding into a fresh
// FileMetaData leaves them nil. Both states are semantically equivalent
// (zero-length), but programs distinguishing nil from empty slices, or
// re-encoding the value, can observe the difference.
func (m *FileMetaData) Reset() {
	m.Version = 0
	for i := range m.Schema {
		m.Schema[i].Reset()
	}
	m.Schema = m.Schema[:0]
	m.NumRows = 0
	for i := range m.RowGroups {
		m.RowGroups[i].Reset()
	}
	m.RowGroups = m.RowGroups[:0]
	clear(m.KeyValueMetadata)
	m.KeyValueMetadata = m.KeyValueMetadata[:0]
	m.CreatedBy = ""
	m.ColumnOrders = m.ColumnOrders[:0]
	// Retained union member; scrub buffer-aliasing contents (see file header).
	switch v := m.EncryptionAlgorithm.Value.(type) {
	case *AesGcmV1:
		*v = AesGcmV1{}
	case *AesGcmCtrV1:
		*v = AesGcmCtrV1{}
	}
	m.FooterSigningKeyMetadata = nil
}

// Reset clears s in place, retaining allocated capacity for reuse in
// subsequent thrift decodes.
func (s *SchemaElement) Reset() {
	s.Type.Reset()
	s.TypeLength.Reset()
	s.RepetitionType.Reset()
	s.Name = ""
	s.NumChildren.Reset()
	s.ConvertedType.Reset()
	s.Scale.Reset()
	s.Precision.Reset()
	s.FieldID = 0
	// Retained union member; scrub the buffer-aliasing CRS strings of the
	// Geometry and Geography members (see file header).
	switch m := s.LogicalType.Value.(type) {
	case *GeometryType:
		*m = GeometryType{}
	case *GeographyType:
		*m = GeographyType{}
	}
}

// Reset clears c in place, retaining allocated capacity for reuse in
// subsequent thrift decodes.
func (c *ColumnChunk) Reset() {
	c.FilePath = ""
	c.FileOffset = 0
	c.MetaData.Reset()
	c.OffsetIndexOffset = 0
	c.OffsetIndexLength = 0
	c.ColumnIndexOffset = 0
	c.ColumnIndexLength = 0
	// Retained union member; scrub buffer-aliasing contents (see file header).
	if v, ok := c.CryptoMetadata.Value.(*EncryptionWithColumnKey); ok {
		*v = EncryptionWithColumnKey{}
	}
	c.EncryptedColumnMetadata = nil
}

// Reset clears c in place, retaining allocated capacity for reuse in
// subsequent thrift decodes.
func (c *ColumnMetaData) Reset() {
	c.Type = 0
	c.Encoding = c.Encoding[:0]
	clear(c.PathInSchema)
	c.PathInSchema = c.PathInSchema[:0]
	c.Codec = 0
	c.NumValues = 0
	c.TotalUncompressedSize = 0
	c.TotalCompressedSize = 0
	clear(c.KeyValueMetadata)
	c.KeyValueMetadata = c.KeyValueMetadata[:0]
	c.DataPageOffset = 0
	c.IndexPageOffset = 0
	c.DictionaryPageOffset = 0
	// Statistics byte slices alias the input buffer (replaced, never
	// appended to), so zero the whole struct instead of truncating.
	c.Statistics = Statistics{}
	clear(c.EncodingStats)
	c.EncodingStats = c.EncodingStats[:0]
	c.BloomFilterOffset = 0
	c.BloomFilterLength = 0
	// The histograms and geospatial types retain capacity.
	c.SizeStatistics.UnencodedByteArrayDataBytes = 0
	c.SizeStatistics.RepetitionLevelHistogram = c.SizeStatistics.RepetitionLevelHistogram[:0]
	c.SizeStatistics.DefinitionLevelHistogram = c.SizeStatistics.DefinitionLevelHistogram[:0]
	c.GeospatialStatistics.BBox = BoundingBox{}
	c.GeospatialStatistics.GeoSpatialTypes = c.GeospatialStatistics.GeoSpatialTypes[:0]
}

package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"github.com/golang/snappy"
)

type ChunkHeader struct {
	// These two fields will be missing from older chunks (as will the hash).
	// On fetch we will initialise these fields from the DynamoDB key.
	Fingerprint uint64 `json:"fingerprint"` // model.Fingerprint
	UserID      string `json:"userID"`

	// These fields will be in all chunks, including old ones.
	From    Time   `json:"from"`    // model.Time
	Through Time   `json:"through"` // model.Time
	Metric  Labels `json:"metric"`

	// We never use Delta encoding (the zero value), so if this entry is
	// missing, we default to DoubleDelta.
	Encoding byte `json:"encoding"`

	MetadataLength uint32
	DataLength     uint32
}

// Decode the chunk from the given buffer, and confirm the chunk is the one we
// expected.
func DecodeHeader(r io.Reader) (*ChunkHeader, error) {
	// Now unmarshal the chunk metadata.
	var metadataLen uint32
	if err := binary.Read(r, binary.BigEndian, &metadataLen); err != nil {
		return nil, fmt.Errorf("when reading metadata length from chunk: %w", err)
	}

	metadataBytes := make([]byte, metadataLen-4) // writer includes size of "metadataLen" in the length
	if _, err := io.ReadFull(r, metadataBytes); err != nil {
		return nil, fmt.Errorf("error while reading metadata bytes: %w", err)
	}

	var metadata ChunkHeader
	if err := json.NewDecoder(snappy.NewReader(bytes.NewReader(metadataBytes))).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("error while decoding metadata: %w", err)
	}

	var dataLen uint32
	if err := binary.Read(r, binary.BigEndian, &dataLen); err != nil {
		return nil, fmt.Errorf("when reading rawData length from chunk: %w", err)
	}

	metadata.MetadataLength = metadataLen
	metadata.DataLength = dataLen

	return &metadata, nil
}

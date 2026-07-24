package magic

import "bytes"

// Sqlite matches an SQLite database file.
func Sqlite(raw []byte, _ uint32) bool {
	return bytes.HasPrefix(raw, []byte{
		0x53, 0x51, 0x4c, 0x69, 0x74, 0x65, 0x20, 0x66,
		0x6f, 0x72, 0x6d, 0x61, 0x74, 0x20, 0x33, 0x00,
	})
}

// MsAccessAce matches Microsoft Access dababase file.
func MsAccessAce(raw []byte, _ uint32) bool {
	return offset(raw, []byte("Standard ACE DB"), 4)
}

// MsAccessMdb matches legacy Microsoft Access database file (JET, 2003 and earlier).
func MsAccessMdb(raw []byte, _ uint32) bool {
	return offset(raw, []byte("Standard Jet DB"), 4)
}

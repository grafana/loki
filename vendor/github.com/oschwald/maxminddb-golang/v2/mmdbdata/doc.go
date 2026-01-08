// Package mmdbdata provides low-level types and interfaces for custom MaxMind DB decoding.
//
// This package allows custom decoding logic for applications that need fine-grained
// control over how MaxMind DB data is processed. For most use cases, the high-level
// maxminddb.Reader API is recommended instead.
//
// # Manual Decoding Example
//
// Custom types can implement the Unmarshaler interface for custom decoding:
//
//	type City struct {
//		Names     map[string]string `maxminddb:"names"`
//		GeoNameID uint              `maxminddb:"geoname_id"`
//	}
//
//	func (c *City) UnmarshalMaxMindDB(d *mmdbdata.Decoder) error {
//		mapIter, _, err := d.ReadMap()
//		if err != nil { return err }
//		for key, err := range mapIter {
//			if err != nil { return err }
//			switch string(key) {
//			case "names":
//				nameIter, size, err := d.ReadMap()
//				if err != nil { return err }
//				names := make(map[string]string, size) // Pre-allocate with size
//				for nameKey, nameErr := range nameIter {
//					if nameErr != nil { return nameErr }
//					value, valueErr := d.ReadString()
//					if valueErr != nil { return valueErr }
//					names[string(nameKey)] = value
//				}
//				c.Names = names
//			case "geoname_id":
//				geoID, err := d.ReadUint32()
//				if err != nil { return err }
//				c.GeoNameID = uint(geoID)
//			default:
//				if err := d.SkipValue(); err != nil { return err }
//			}
//		}
//		return nil
//	}
//
// Types implementing Unmarshaler will automatically use custom decoding logic
// instead of reflection when used with maxminddb.Reader.Lookup, similar to how
// json.Unmarshaler works with encoding/json.
//
// # Direct Decoder Usage
//
// For even more control, you can use the Decoder directly:
//
//	decoder := mmdbdata.NewDecoder(buffer, offset)
//	value, err := decoder.ReadString()
//	if err != nil {
//		return err
//	}
package mmdbdata

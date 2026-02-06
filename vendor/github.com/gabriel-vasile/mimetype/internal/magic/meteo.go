package magic

import "bytes"

// GRIB matches a GRIdded Binary meteorological file.
// https://www.nco.ncep.noaa.gov/pmb/docs/on388/
// https://www.nco.ncep.noaa.gov/pmb/docs/grib2/grib2_doc/
func GRIB(raw []byte, _ uint32) bool {
	return len(raw) > 7 &&
		bytes.HasPrefix(raw, []byte("GRIB")) &&
		(raw[7] == 1 || raw[7] == 2)
}

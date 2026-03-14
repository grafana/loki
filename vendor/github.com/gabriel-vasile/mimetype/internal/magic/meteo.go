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

// BUFR matches meteorological data format for storing point or time series data.
// https://confluence.ecmwf.int/download/attachments/31064617/ecCodes_BUFR_in_a_nutshell.pdf?version=1&modificationDate=1457000352419&api=v2
func BUFR(raw []byte, _ uint32) bool {
	return len(raw) > 7 &&
		bytes.HasPrefix(raw, []byte("BUFR")) &&
		(raw[7] == 0x03 || raw[7] == 0x04)
}

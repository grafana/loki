package magic

import (
	"github.com/gabriel-vasile/mimetype/internal/csv"
	"github.com/gabriel-vasile/mimetype/internal/scan"
)

// CSV matches a comma-separated values file.
func CSV(raw []byte, limit uint32) bool {
	return sv(raw, ',', limit)
}

// TSV matches a tab-separated values file.
func TSV(raw []byte, limit uint32) bool {
	return sv(raw, '\t', limit)
}

func sv(in []byte, comma byte, limit uint32) bool {
	s := scan.Bytes(in)
	r := csv.NewParser(comma, '#', &s)

	headerFields, _, hasMore := r.CountFields(false)
	if headerFields < 2 || !hasMore {
		return false
	}
	csvLines := 1 // 1 for header
	for {
		fields, _, hasMore := r.CountFields(false)
		if !hasMore && fields == 0 {
			break
		}
		if fields == headerFields {
			csvLines++
		} else {
			// maybeTruncated signals the input was cut at the read limit,
			// meaning the last line may be an incomplete CSV record.
			maybeTruncated := limit > 0 && uint64(len(in)) >= uint64(limit)
			if maybeTruncated && fields < headerFields {
				// Allow the last row to have any number of fields
				// if the input is maybeTruncated.
				// BUG: if len(input) == limit, then the input is not truncated
				// but it is still allowed to have the wrong number of fields
				// and it will be reported as valid CSV.
				if len(s) == 0 {
					break
				}
			}
			return false
		}
		if csvLines >= 10 {
			return true
		}
	}

	return csvLines >= 2
}

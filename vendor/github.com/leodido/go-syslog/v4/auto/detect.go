package auto

// detect classifies input as RFC3164 or RFC5424 using narrow leading-byte
// signatures. Inputs beginning with '<' retain the existing post-PRI heuristic;
// inputs without PRI use timestamp or VERSION signatures.
func detect(input []byte) Format {
	if len(input) == 0 || input[0] != '<' {
		return detectWithoutPRI(input)
	}

	// Find the closing '>' of PRI.
	pos := 0
	for pos < len(input) && input[pos] != '>' {
		pos++
	}
	if pos >= len(input) {
		return detectWithoutPRI(input)
	}

	// Move past '>'.
	pos++
	if pos >= len(input) {
		// Nothing after '>' — default to RFC 5424 (will fail with version error).
		return FormatRFC5424
	}

	b := input[pos]

	// Letter, space, or '*' → RFC 3164.
	if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || b == ' ' || b == '*' {
		return FormatRFC3164
	}

	// '0' → RFC 3164 (RFC 5424 version cannot start with 0).
	if b == '0' {
		return FormatRFC3164
	}

	// Digit 1-9: scan forward to distinguish VERSION (5424) from Cisco counter (3164).
	if b >= '1' && b <= '9' {
		digitCount := 1
		pos++
		for pos < len(input) && input[pos] >= '0' && input[pos] <= '9' {
			digitCount++
			pos++
		}

		// VERSION is 1-3 digits. 4+ digits → Cisco message counter → RFC 3164.
		if digitCount > 3 {
			return FormatRFC3164
		}

		// Check the byte after the digit run.
		if pos < len(input) {
			if input[pos] == ' ' {
				return FormatRFC5424
			}
			if input[pos] == ':' {
				return FormatRFC3164
			}
		}

		// Ambiguous or truncated — default to RFC 5424.
		return FormatRFC5424
	}

	// Any other byte — default to RFC 5424 (stricter grammar gives clearer errors).
	return FormatRFC5424
}

func detectWithoutPRI(input []byte) Format {
	if hasRFC3164MonthPrefix(input) {
		return FormatRFC3164
	}
	if len(input) >= 5 &&
		input[0] >= '0' && input[0] <= '9' &&
		input[1] >= '0' && input[1] <= '9' &&
		input[2] >= '0' && input[2] <= '9' &&
		input[3] >= '0' && input[3] <= '9' &&
		input[4] == '-' {
		return FormatRFC3164
	}
	return FormatRFC5424
}

func hasRFC3164MonthPrefix(input []byte) bool {
	if len(input) < 4 || input[3] != ' ' {
		return false
	}

	switch string(input[:3]) {
	case "Jan", "Feb", "Mar", "Apr", "May", "Jun",
		"Jul", "Aug", "Sep", "Oct", "Nov", "Dec":
		return true
	default:
		return false
	}
}

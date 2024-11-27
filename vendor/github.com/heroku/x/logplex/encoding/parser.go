package encoding

import (
	"bufio"
	"bytes"
	"strconv"

	"github.com/pkg/errors"
)

// SyslogSplitFunc splits the data based on the defined length prefix.
// format:
// 64 <190>1 2019-07-20T17:50:10.879238Z shuttle token shuttle - - 99\n65 <190>1 2019-07-20T17:50:10.879238Z shuttle token shuttle - - 100\n
// ^ frame size                                                       ^ boundary
//
//nolint:lll
func SyslogSplitFunc(data []byte, atEOF bool) (advance int, token []byte, err error) {
	// first space gives us the frame size
	sp := bytes.IndexByte(data, ' ')
	if sp == -1 {
		if atEOF && len(data) > 0 {
			return 0, nil, errors.Wrap(ErrBadFrame, "missing frame length")
		}
		return 0, nil, nil
	}

	if sp == 0 {
		return 0, nil, errors.Wrap(ErrBadFrame, "invalid frame length")
	}

	msgSize, err := strconv.Atoi(string(data[0:sp]))
	if err != nil {
		return 0, nil, errors.Wrap(ErrBadFrame, "couldnt parse frame length")
	}

	// 1 here is the 'space' itself, used in the framing above
	dataBoundary := sp + msgSize + 1

	if dataBoundary > len(data) {
		if atEOF {
			return 0, nil, errors.Wrapf(ErrBadFrame, "message boundary (%d) not respected length (%d)", dataBoundary, len(data))
		}
		return 0, nil, nil
	}

	return dataBoundary, data[sp+1 : dataBoundary], nil
}

// TruncatingSyslogSplitFunc enforces a maximum line length after parsing.
func TruncatingSyslogSplitFunc(maxLength int) bufio.SplitFunc {
	return func(data []byte, atEOF bool) (advance int, token []byte, err error) {
		advance, token, err = SyslogSplitFunc(data, atEOF)
		if len(token) > maxLength {
			token = token[0:maxLength]
		}

		return
	}
}

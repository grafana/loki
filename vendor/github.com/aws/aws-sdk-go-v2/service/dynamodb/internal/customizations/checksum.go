package customizations

import (
	"context"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"net/http"
	"strconv"

	smithy "github.com/aws/smithy-go"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// AddValidateResponseChecksumOptions provides the options for the
// AddValidateResponseChecksum middleware setup.
type AddValidateResponseChecksumOptions struct {
	Disable bool
}

// AddValidateResponseChecksum adds the Checksum to the middleware
// stack if checksum is not disabled.
func AddValidateResponseChecksum(stack *middleware.Stack, options AddValidateResponseChecksumOptions) error {
	if options.Disable {
		return nil
	}

	return stack.Deserialize.Add(&Checksum{}, middleware.After)
}

// Checksum provides a middleware to validate the DynamoDB response
// body's integrity by comparing the computed CRC32 checksum with the value
// provided in the HTTP response header.
type Checksum struct{}

// ID returns the middleware ID.
func (*Checksum) ID() string { return "DynamoDB:ResponseChecksumValidation" }

// HandleDeserialize implements the Deserialize middleware handle method.
func (m *Checksum) HandleDeserialize(
	ctx context.Context, input middleware.DeserializeInput, next middleware.DeserializeHandler,
) (
	output middleware.DeserializeOutput, metadata middleware.Metadata, err error,
) {
	output, metadata, err = next.HandleDeserialize(ctx, input)
	if err != nil {
		return output, metadata, err
	}

	resp, ok := output.RawResponse.(*smithyhttp.Response)
	if !ok {
		return output, metadata, &smithy.DeserializationError{
			Err: fmt.Errorf("unknown response type %T", output.RawResponse),
		}
	}

	expectChecksum, ok, err := getCRC32Checksum(resp.Header)
	if err != nil {
		return output, metadata, &smithy.DeserializationError{Err: err}
	}

	resp.Body = wrapCRC32ChecksumValidate(expectChecksum, resp.Body)

	return output, metadata, err
}

const crc32ChecksumHeader = "X-Amz-Crc32"

func getCRC32Checksum(header http.Header) (uint32, bool, error) {
	v := header.Get(crc32ChecksumHeader)
	if len(v) == 0 {
		return 0, false, nil
	}

	c, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return 0, false, fmt.Errorf("unable to parse checksum header %v, %w", v, err)
	}

	return uint32(c), true, nil
}

// crc32ChecksumValidate provides wrapping of an io.Reader to validate the CRC32
// checksum of the bytes read against the expected checksum.
type crc32ChecksumValidate struct {
	io.Reader

	closer io.Closer
	expect uint32
	hash   hash.Hash32
}

// wrapCRC32ChecksumValidate constructs a new crc32ChecksumValidate that will
// compute a running CRC32 checksum of the bytes read.
func wrapCRC32ChecksumValidate(checksum uint32, reader io.ReadCloser) *crc32ChecksumValidate {
	hash := crc32.NewIEEE()
	return &crc32ChecksumValidate{
		expect: checksum,
		Reader: io.TeeReader(reader, hash),
		closer: reader,
		hash:   hash,
	}
}

// Close validates the wrapped reader's CRC32 checksum. Returns an error if
// the read checksum does not match the expected checksum.
//
// May return an error if the wrapped io.Reader's close returns an error, if it
// implements close.
func (c *crc32ChecksumValidate) Close() error {
	if actual := c.hash.Sum32(); actual != c.expect {
		c.closer.Close()
		return fmt.Errorf("response did not match expected checksum, %d, %d", c.expect, actual)
	}

	return c.closer.Close()
}

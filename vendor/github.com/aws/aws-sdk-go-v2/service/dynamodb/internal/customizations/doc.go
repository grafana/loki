/*
Package customizations provides customizations for the Amazon DynamoDB API client.

The DynamoDB API client uses two customizations, response checksum validation,
and manual content-encoding: gzip support.

# Middleware layering

Checksum validation needs to be performed first in deserialization chain
on top of gzip decompression. Since the behavior of Deserialization is
in reverse order to the other stack steps its easier to consider that
"after" means "before".

	HTTP Response -> Checksum ->  gzip decompress -> deserialize

# Response checksum validation

DynamoDB responses can include a X-Amz-Crc32 header with the CRC32 checksum
value of the response body. If the response body is content-encoding: gzip, the
checksum is of the gzipped response content.

If the header is present, the SDK should validate that the response payload
computed CRC32 checksum matches the value provided in the header. The checksum
header is based on the original payload provided returned by the service. Which
means that if the response is gzipped the checksum is of the gzipped response,
not the decompressed response bytes.

Customization option:

	DisableValidateResponseChecksum (Enabled by Default)

# Accept encoding gzip

For customization around accept encoding, dynamodb client uses the middlewares
defined at service/internal/accept-encoding. Please refer to the documentation for
`accept-encoding` package for more details.

Customization option:

	EnableAcceptEncodingGzip (Disabled by Default)
*/
package customizations

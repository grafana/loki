package api

import (
	"fmt"
	"strconv"
	"time"

	"github.com/baidubce/bce-sdk-go/bce"
	"github.com/baidubce/bce-sdk-go/http"
	"github.com/baidubce/bce-sdk-go/util"
)

type optionType string

const (
	optionHeader optionType = "HttpHeader"    // HTTP Header
	optionParam  optionType = "HttpParameter" // URL parameter
)

type (
	optionValue struct {
		Value interface{}
		Type  optionType
	}
	// Set various options of HTTP request
	Option func(params map[string]optionValue) error
)

// An option to set Cache-Control header
func CacheControl(value string) Option {
	return setHeader(http.CACHE_CONTROL, value)
}

// An option to set Content-Disposition header
func ContentDisposition(value string) Option {
	return setHeader(http.CONTENT_DISPOSITION, value)
}

// An option to set Content-Encoding header
func ContentEncoding(value string) Option {
	return setHeader(http.CONTENT_ENCODING, value)
}

// An option to set Content-Language header
func ContentLanguage(value string) Option {
	return setHeader(http.CONTENT_LANGUAGE, value)
}

// An option to set Content-Length header
func ContentLength(length int64) Option {
	return setHeader(http.CONTENT_LENGTH, strconv.FormatInt(length, 10))
}

// An option to set Content-Md5 header
func ContentMD5(value string) Option {
	return setHeader(http.CONTENT_MD5, value)
}

// An option to set Content-Range header
func ContentRange(start, end int64) Option {
	return setHeader(http.CONTENT_RANGE, fmt.Sprintf("bytes=%d-%d", start, end))
}

// An option to set Range header
func Range(start, end int64) Option {
	return setHeader(http.RANGE, fmt.Sprintf("bytes=%d-%d", start, end))
}

// An option to set Content-Type header
func ContentType(value string) Option {
	return setHeader(http.CONTENT_TYPE, value)
}

// An option to set Date header
func Date(t time.Time) Option {
	return setHeader(http.DATE, util.FormatRFC822Date(t.UTC().Unix()))
}

// An option to set Expires header
func Expires(t time.Time) Option {
	return setHeader(http.EXPIRES, util.FormatRFC822Date(t.UTC().Unix()))
}

// An option to set User-Agent header
func UserAgent(userAgent string) Option {
	return setHeader(http.USER_AGENT, userAgent)
}

// An option to set X-Bce-Acl header
func CannedAcl(cannedAcl string) Option {
	if !validCannedAcl(cannedAcl) {
		return nil
	}
	return setHeader(http.BCE_ACL, cannedAcl)
}

// An option to set X-Bce-Grant-Read header
func GrantRead(ids []string) Option {
	if len(ids) == 0 {
		return nil
	}
	return setHeader(http.BCE_GRANT_READ, joinUserIds(ids))
}

// An option to set X-Bce-Grant-Full-Control header
func GrantFullControl(ids []string) Option {
	if len(ids) == 0 {
		return nil
	}
	return setHeader(http.BCE_GRANT_FULL_CONTROL, joinUserIds(ids))
}

// An option to set X-Bce-Content-Sha256 header
func ContentSha256(value string) Option {
	return setHeader(http.BCE_CONTENT_SHA256, value)
}

// An option to set X-Bce-Content-Crc32 header
func ContentCrc32(crc32 uint32) Option {
	return setHeader(http.BCE_CONTENT_CRC32, strconv.FormatUint(uint64(crc32), 10))
}

// An option to set X-Bce-Content-Crc32c header
func ContentCrc32c(crc32c uint32) Option {
	return setHeader(http.BCE_CONTENT_CRC32, strconv.FormatUint(uint64(crc32c), 10))
}

// An option to set X-Bce-Content-Crc32c-Flag header
func ContentCrc32cFlag(crc32cFlag bool) Option {
	if !crc32cFlag {
		return nil
	}
	return setHeader(http.BCE_CONTENT_CRC32, strconv.FormatBool(crc32cFlag))
}

// An option to set X-Bce-Meta-* header
func UserMeta(key, value string) Option {
	return setHeader(http.BCE_USER_METADATA_PREFIX+key, value)
}

// An option to set X-Bce-Security-Token header
func SecurityToken(value string) Option {
	return setHeader(http.BCE_SECURITY_TOKEN, value)
}

// An option to set X-Bce-Date header
func BceDate(t time.Time) Option {
	return setHeader(http.BCE_DATE, util.FormatISO8601Date(t.UTC().Unix()))
}

// An option to set X-Bce-Tag-List header
func TagList(tags map[string]string) Option {
	tagsStr := taggingMapToStr(tags)
	if tagsStr == "" {
		return nil
	}
	return setHeader(http.BCE_TAG, tagsStr)
}

// An option to set x-bce-metadata-directive header
func MetadataDirective(value string) Option {
	return setHeader(http.BCE_COPY_METADATA_DIRECTIVE, value)
}

// An option to set x-bce-tagging-directive header
func TaggingDirective(value string) Option {
	return setHeader(http.BCE_COPY_TAGGING_DIRECTIVE, value)
}

// An option to set x-bce-copy-source header
func CopySource(srcBucket, srcObject string) Option {
	return setHeader(http.BCE_COPY_SOURCE, "/"+srcBucket+"/"+srcObject)
}

// An option to set x-bce-copy-source-if-match header
func CopySourceIfMatch(value string) Option {
	return setHeader(http.BCE_COPY_SOURCE_IF_MATCH, value)
}

// An option to set x-bce-copy-source-if-none-match header
func CopySourceIfNoneMatch(value string) Option {
	return setHeader(http.BCE_COPY_SOURCE_IF_NONE_MATCH, value)
}

// An option to set x-bce-copy-source-if-modified-since header
func CopySourceIfModifiedSince(t time.Time) Option {
	return setHeader(http.BCE_COPY_SOURCE_IF_MODIFIED_SINCE, util.FormatRFC822Date(t.UTC().Unix()))
}

// An option to set x-bce-copy-source-if-unmodified-since header
func CopySourceIfUnmodifiedSince(t time.Time) Option {
	return setHeader(http.BCE_COPY_SOURCE_IF_UNMODIFIED_SINCE, util.FormatRFC822Date(t.UTC().Unix()))
}

// An option to set x-bce-copy-source-range header
func CopySourceRange(start, end int64) Option {
	return setHeader(http.BCE_COPY_SOURCE_RANGE, fmt.Sprintf("bytes=%d-%d", start, end))
}

// An option to set x-bce-storage-class header
func StorageClass(value string) Option {
	return setHeader(http.BCE_STORAGE_CLASS, value)
}

// An option to set x-bce-process header
func Process(value string) Option {
	return setHeader(http.BCE_PROCESS, value)
}

// An option to set x-bce-restore-tier header
// Expedited：加急取回，30min内完成取回
// Standard：标准取回，2~5小时内完成取回
// LowCost：延缓取回，12小时内完成取回
func RestoreTier(value string) Option {
	return setHeader(http.BCE_RESTORE_TIER, value)
}

// An option to set x-bce-restore-days header
func RestoreDays(days int) Option {
	if days <= 0 {
		return nil
	}
	return setHeader(http.BCE_RESTORE_DAYS, strconv.FormatInt(int64(days), 10))
}

// An options to set x-bce-forbid-overwrite header
func ForbidOverwrite(forbidOverwrite bool) Option {
	if !forbidOverwrite {
		return nil
	}
	return setHeader(http.BCE_FORBID_OVERWRITE, strconv.FormatBool(forbidOverwrite))
}

// An option to set x-bce-symlink-target header
func SymlinkTarget(targetObjectKey string) Option {
	return setHeader(http.BCE_SYMLINK_TARGET, targetObjectKey)
}

// An option to set x-bce-symlink-bucket header
func SymlinkBucket(targetBucket string) Option {
	return setHeader(http.BCE_SYMLINK_BUCKET, targetBucket)
}

// An option to set x-bce-traffic-limit header
func TrafficLimit(value int64) Option {
	if value <= 0 {
		return nil
	}
	return setHeader(http.BCE_TRAFFIC_LIMIT, strconv.FormatInt(value, 10))
}

// An option to set X-Bce-Tagging header
func Tagging(tags map[string]string) Option {
	tagsStr := taggingMapToStr(tags)
	if tagsStr == "" {
		return nil
	}
	return setHeader(http.BCE_OBJECT_TAGGING, tagsStr)
}

// An option to set x-bce-callback-address header
func CallbackAddress(value string) Option {
	return setHeader(http.BCE_FETCH_CALLBACK_ADDRESS, value)
}

// An option to set x-bce-version-id header
func VersionId(value string) Option {
	return setHeader(http.BCE_VERSION_ID, value)
}

// An option to set x-bce-object-expires header
func ObjectExpires(days int) Option {
	if days <= 0 {
		return nil
	}
	return setHeader(http.BCE_OBJECT_EXPIRES, strconv.FormatInt(int64(days), 10))
}

// An option to set x-bce-server-side-encryption header, "AES256" or "SM4"
func ServerSideEncryption(value string) Option {
	return setHeader(http.BCE_SERVER_SIDE_ENCRYPTION, value)
}

// An option to set x-bce-server-side-encryption-customer-key header
func SSECKey(value string) Option {
	return setHeader(http.BCE_SERVER_SIDE_ENCRYPTION_KEY, value)
}

// An option to set x-bce-server-side-encryption-customer-key-md5 header
func SSECKeyMd5(value string) Option {
	return setHeader(http.BCE_SERVER_SIDE_ENCRYPTION_KEY_MD5, value)
}

// An option to set x-bce-server-side-encryption-bos-key-id header
func SSEKmsKeyId(value string) Option {
	return setHeader(http.BCE_SERVER_SIDE_ENCRYPTION_KEY_ID, value)
}

func SetHeader(key string, value interface{}) Option {
	return setHeader(key, value)
}

func SetParam(key string, value interface{}) Option {
	return setParam(key, value)
}

func setHeader(key string, value interface{}) Option {
	if str, ok := value.(string); ok && str == "" {
		return nil
	}
	return func(params map[string]optionValue) error {
		if value == nil {
			return nil
		}
		params[key] = optionValue{value, optionHeader}
		return nil
	}
}

func setParam(key string, value interface{}) Option {
	return func(params map[string]optionValue) error {
		if value == nil {
			return nil
		}
		params[key] = optionValue{value, optionParam}
		return nil
	}
}

func handleOptions(request *BosRequest, options []Option) error {
	params := make(map[string]optionValue)
	for _, option := range options {
		if option != nil {
			if err := option(params); err != nil {
				return err
			}
		}
	}

	aclMethods := 0
	for k, v := range params {
		if v.Type == optionHeader {
			if isAclHeaderkey(k) {
				aclMethods++
			}
			request.SetHeader(k, v.Value.(string))
		} else if v.Type == optionParam {
			request.SetParam(k, v.Value.(string))
		}
	}
	if aclMethods > 1 {
		return bce.NewBceClientError("BOS only support one acl setting method at the same time")
	}
	return nil
}

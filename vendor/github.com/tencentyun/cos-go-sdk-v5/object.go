package cos

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/crc64"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ObjectService 相关 API
type ObjectService service

// ObjectGetOptions is the option of GetObject
type ObjectGetOptions struct {
	ResponseContentType        string `url:"response-content-type,omitempty" header:"-"`
	ResponseContentLanguage    string `url:"response-content-language,omitempty" header:"-"`
	ResponseExpires            string `url:"response-expires,omitempty" header:"-"`
	ResponseCacheControl       string `url:"response-cache-control,omitempty" header:"-"`
	ResponseContentDisposition string `url:"response-content-disposition,omitempty" header:"-"`
	ResponseContentEncoding    string `url:"response-content-encoding,omitempty" header:"-"`
	Range                      string `url:"-" header:"Range,omitempty"`
	IfModifiedSince            string `url:"-" header:"If-Modified-Since,omitempty"`
	// SSE-C
	XCosSSECustomerAglo   string `header:"x-cos-server-side-encryption-customer-algorithm,omitempty" url:"-" xml:"-"`
	XCosSSECustomerKey    string `header:"x-cos-server-side-encryption-customer-key,omitempty" url:"-" xml:"-"`
	XCosSSECustomerKeyMD5 string `header:"x-cos-server-side-encryption-customer-key-MD5,omitempty" url:"-" xml:"-"`

	//兼容其他自定义头部
	XOptionHeader    *http.Header `header:"-,omitempty" url:"-" xml:"-"`
	XCosTrafficLimit int          `header:"x-cos-traffic-limit,omitempty" url:"-" xml:"-"`

	// 下载进度, ProgressCompleteEvent不能表示对应API调用成功，API是否调用成功的判断标准为返回err==nil
	Listener ProgressListener `header:"-" url:"-" xml:"-"`
}

// presignedURLTestingOptions is the opt of presigned url
type presignedURLTestingOptions struct {
	authTime *AuthTime
}

// Get Object 请求可以将一个文件（Object）下载至本地。
// 该操作需要对目标 Object 具有读权限或目标 Object 对所有人都开放了读权限（公有读）。
//
// https://www.qcloud.com/document/product/436/7753
func (s *ObjectService) Get(ctx context.Context, name string, opt *ObjectGetOptions, id ...string) (*Response, error) {
	var u string
	if len(id) == 1 {
		u = fmt.Sprintf("/%s?versionId=%s", encodeURIComponent(name), id[0])
	} else if len(id) == 0 {
		u = "/" + encodeURIComponent(name)
	} else {
		return nil, errors.New("wrong params")
	}

	sendOpt := sendOptions{
		baseURL:          s.client.BaseURL.BucketURL,
		uri:              u,
		method:           http.MethodGet,
		optQuery:         opt,
		optHeader:        opt,
		disableCloseBody: true,
	}
	resp, err := s.client.doRetry(ctx, &sendOpt)

	if opt != nil && opt.Listener != nil {
		if err == nil && resp != nil {
			if totalBytes, e := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); e == nil {
				resp.Body = TeeReader(resp.Body, nil, totalBytes, opt.Listener)
			}
		}
	}
	return resp, err
}

// GetToFile download the object to local file
func (s *ObjectService) GetToFile(ctx context.Context, name, localpath string, opt *ObjectGetOptions, id ...string) (*Response, error) {
	resp, err := s.Get(ctx, name, opt, id...)
	if err != nil {
		return resp, err
	}
	defer resp.Body.Close()

	// If file exist, overwrite it
	fd, err := os.OpenFile(localpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0660)
	if err != nil {
		return resp, err
	}

	_, err = io.Copy(fd, resp.Body)
	fd.Close()
	if err != nil {
		return resp, err
	}

	return resp, nil
}

func (s *ObjectService) GetObjectURL(name string) *url.URL {
	uri, _ := url.Parse("/" + encodeURIComponent(name, []byte{'/'}))
	return s.client.BaseURL.BucketURL.ResolveReference(uri)
}

type PresignedURLOptions struct {
	Query  *url.Values  `xml:"-" url:"-" header:"-"`
	Header *http.Header `header:"-,omitempty" url:"-" xml:"-"`
}

// GetPresignedURL get the object presigned to down or upload file by url
func (s *ObjectService) GetPresignedURL(ctx context.Context, httpMethod, name, ak, sk string, expired time.Duration, opt interface{}) (*url.URL, error) {
	sendOpt := sendOptions{
		baseURL:   s.client.BaseURL.BucketURL,
		uri:       "/" + encodeURIComponent(name),
		method:    httpMethod,
		optQuery:  opt,
		optHeader: opt,
	}
	if popt, ok := opt.(*PresignedURLOptions); ok {
		qs := popt.Query.Encode()
		if qs != "" {
			sendOpt.uri = fmt.Sprintf("%s?%s", sendOpt.uri, qs)
		}
	}
	req, err := s.client.newRequest(ctx, sendOpt.baseURL, sendOpt.uri, sendOpt.method, sendOpt.body, sendOpt.optQuery, sendOpt.optHeader)
	if err != nil {
		return nil, err
	}

	var authTime *AuthTime
	if opt != nil {
		if opt, ok := opt.(*presignedURLTestingOptions); ok {
			authTime = opt.authTime
		}
	}
	if authTime == nil {
		authTime = NewAuthTime(expired)
	}
	authorization := newAuthorization(ak, sk, req, authTime)
	sign := encodeURIComponent(authorization, []byte{'&', '='})

	if req.URL.RawQuery == "" {
		req.URL.RawQuery = fmt.Sprintf("%s", sign)
	} else {
		req.URL.RawQuery = fmt.Sprintf("%s&%s", req.URL.RawQuery, sign)
	}
	return req.URL, nil
}

// ObjectPutHeaderOptions the options of header of the put object
type ObjectPutHeaderOptions struct {
	CacheControl       string `header:"Cache-Control,omitempty" url:"-"`
	ContentDisposition string `header:"Content-Disposition,omitempty" url:"-"`
	ContentEncoding    string `header:"Content-Encoding,omitempty" url:"-"`
	ContentType        string `header:"Content-Type,omitempty" url:"-"`
	ContentMD5         string `header:"Content-MD5,omitempty" url:"-"`
	ContentLength      int64  `header:"Content-Length,omitempty" url:"-"`
	ContentLanguage    string `header:"Content-Language,omitempty" url:"-"`
	Expect             string `header:"Expect,omitempty" url:"-"`
	Expires            string `header:"Expires,omitempty" url:"-"`
	XCosContentSHA1    string `header:"x-cos-content-sha1,omitempty" url:"-"`
	// 自定义的 x-cos-meta-* header
	XCosMetaXXX      *http.Header `header:"x-cos-meta-*,omitempty" url:"-"`
	XCosStorageClass string       `header:"x-cos-storage-class,omitempty" url:"-"`
	// 可选值: Normal, Appendable
	//XCosObjectType string `header:"x-cos-object-type,omitempty" url:"-"`
	// Enable Server Side Encryption, Only supported: AES256
	XCosServerSideEncryption string `header:"x-cos-server-side-encryption,omitempty" url:"-" xml:"-"`
	// SSE-C
	XCosSSECustomerAglo   string `header:"x-cos-server-side-encryption-customer-algorithm,omitempty" url:"-" xml:"-"`
	XCosSSECustomerKey    string `header:"x-cos-server-side-encryption-customer-key,omitempty" url:"-" xml:"-"`
	XCosSSECustomerKeyMD5 string `header:"x-cos-server-side-encryption-customer-key-MD5,omitempty" url:"-" xml:"-"`
	//兼容其他自定义头部
	XOptionHeader    *http.Header `header:"-,omitempty" url:"-" xml:"-"`
	XCosTrafficLimit int          `header:"x-cos-traffic-limit,omitempty" url:"-" xml:"-"`

	// 上传进度, ProgressCompleteEvent不能表示对应API调用成功，API是否调用成功的判断标准为返回err==nil
	Listener ProgressListener `header:"-" url:"-" xml:"-"`
}

// ObjectPutOptions the options of put object
type ObjectPutOptions struct {
	*ACLHeaderOptions       `header:",omitempty" url:"-" xml:"-"`
	*ObjectPutHeaderOptions `header:",omitempty" url:"-" xml:"-"`
}

// Put Object请求可以将一个文件（Oject）上传至指定Bucket。
//
// https://www.qcloud.com/document/product/436/7749
func (s *ObjectService) Put(ctx context.Context, name string, r io.Reader, uopt *ObjectPutOptions) (*Response, error) {
	if r == nil {
		return nil, fmt.Errorf("reader is nil")
	}
	if err := CheckReaderLen(r); err != nil {
		return nil, err
	}
	opt := CloneObjectPutOptions(uopt)
	totalBytes, err := GetReaderLen(r)
	if err != nil && opt != nil && opt.Listener != nil {
		if opt.ContentLength == 0 {
			return nil, err
		}
		totalBytes = opt.ContentLength
	}
	if err == nil {
		// 与 go http 保持一致, 非bytes.Buffer/bytes.Reader/strings.Reader由用户指定ContentLength, 或使用 Chunk 上传
		if opt != nil && opt.ContentLength == 0 && IsLenReader(r) {
			opt.ContentLength = totalBytes
		}
	}
	reader := TeeReader(r, nil, totalBytes, nil)
	if s.client.Conf.EnableCRC {
		reader.writer = crc64.New(crc64.MakeTable(crc64.ECMA))
	}
	if opt != nil && opt.Listener != nil {
		reader.listener = opt.Listener
	}
	sendOpt := sendOptions{
		baseURL:   s.client.BaseURL.BucketURL,
		uri:       "/" + encodeURIComponent(name),
		method:    http.MethodPut,
		body:      reader,
		optHeader: opt,
	}
	resp, err := s.client.send(ctx, &sendOpt)

	return resp, err
}

// PutFromFile put object from local file
func (s *ObjectService) PutFromFile(ctx context.Context, name string, filePath string, opt *ObjectPutOptions) (resp *Response, err error) {
	nr := 0
	for nr < 3 {
		fd, e := os.Open(filePath)
		if e != nil {
			err = e
			return
		}
		resp, err = s.Put(ctx, name, fd, opt)
		if err != nil {
			nr++
			fd.Close()
			continue
		}
		fd.Close()
		break
	}
	return
}

// ObjectCopyHeaderOptions is the head option of the Copy
type ObjectCopyHeaderOptions struct {
	// When use replace directive to update meta infos
	CacheControl                    string `header:"Cache-Control,omitempty" url:"-"`
	ContentDisposition              string `header:"Content-Disposition,omitempty" url:"-"`
	ContentEncoding                 string `header:"Content-Encoding,omitempty" url:"-"`
	ContentLanguage                 string `header:"Content-Language,omitempty" url:"-"`
	ContentType                     string `header:"Content-Type,omitempty" url:"-"`
	Expires                         string `header:"Expires,omitempty" url:"-"`
	Expect                          string `header:"Expect,omitempty" url:"-"`
	XCosMetadataDirective           string `header:"x-cos-metadata-directive,omitempty" url:"-" xml:"-"`
	XCosCopySourceIfModifiedSince   string `header:"x-cos-copy-source-If-Modified-Since,omitempty" url:"-" xml:"-"`
	XCosCopySourceIfUnmodifiedSince string `header:"x-cos-copy-source-If-Unmodified-Since,omitempty" url:"-" xml:"-"`
	XCosCopySourceIfMatch           string `header:"x-cos-copy-source-If-Match,omitempty" url:"-" xml:"-"`
	XCosCopySourceIfNoneMatch       string `header:"x-cos-copy-source-If-None-Match,omitempty" url:"-" xml:"-"`
	XCosStorageClass                string `header:"x-cos-storage-class,omitempty" url:"-" xml:"-"`
	// 自定义的 x-cos-meta-* header
	XCosMetaXXX              *http.Header `header:"x-cos-meta-*,omitempty" url:"-"`
	XCosCopySource           string       `header:"x-cos-copy-source" url:"-" xml:"-"`
	XCosServerSideEncryption string       `header:"x-cos-server-side-encryption,omitempty" url:"-" xml:"-"`
	// SSE-C
	XCosSSECustomerAglo             string `header:"x-cos-server-side-encryption-customer-algorithm,omitempty" url:"-" xml:"-"`
	XCosSSECustomerKey              string `header:"x-cos-server-side-encryption-customer-key,omitempty" url:"-" xml:"-"`
	XCosSSECustomerKeyMD5           string `header:"x-cos-server-side-encryption-customer-key-MD5,omitempty" url:"-" xml:"-"`
	XCosCopySourceSSECustomerAglo   string `header:"x-cos-copy-source-server-side-encryption-customer-algorithm,omitempty" url:"-" xml:"-"`
	XCosCopySourceSSECustomerKey    string `header:"x-cos-copy-source-server-side-encryption-customer-key,omitempty" url:"-" xml:"-"`
	XCosCopySourceSSECustomerKeyMD5 string `header:"x-cos-copy-source-server-side-encryption-customer-key-MD5,omitempty" url:"-" xml:"-"`
	//兼容其他自定义头部
	XOptionHeader *http.Header `header:"-,omitempty" url:"-" xml:"-"`
}

// ObjectCopyOptions is the option of Copy, choose header or body
type ObjectCopyOptions struct {
	*ObjectCopyHeaderOptions `header:",omitempty" url:"-" xml:"-"`
	*ACLHeaderOptions        `header:",omitempty" url:"-" xml:"-"`
}

// ObjectCopyResult is the result of Copy
type ObjectCopyResult struct {
	XMLName      xml.Name `xml:"CopyObjectResult"`
	ETag         string   `xml:"ETag,omitempty"`
	LastModified string   `xml:"LastModified,omitempty"`
	CRC64        string   `xml:"CRC64,omitempty"`
	VersionId    string   `xml:"VersionId,omitempty"`
}

// Copy 调用 PutObjectCopy 请求实现将一个文件从源路径复制到目标路径。建议文件大小 1M 到 5G，
// 超过 5G 的文件请使用分块上传 Upload - Copy。在拷贝的过程中，文件元属性和 ACL 可以被修改。
//
// 用户可以通过该接口实现文件移动，文件重命名，修改文件属性和创建副本。
//
// 注意：在跨帐号复制的时候，需要先设置被复制文件的权限为公有读，或者对目标帐号赋权，同帐号则不需要。
//
// https://cloud.tencent.com/document/product/436/10881
func (s *ObjectService) Copy(ctx context.Context, name, sourceURL string, opt *ObjectCopyOptions, id ...string) (*ObjectCopyResult, *Response, error) {
	surl := strings.SplitN(sourceURL, "/", 2)
	if len(surl) < 2 {
		return nil, nil, errors.New(fmt.Sprintf("x-cos-copy-source format error: %s", sourceURL))
	}
	var u string
	if len(id) == 1 {
		u = fmt.Sprintf("%s/%s?versionId=%s", surl[0], encodeURIComponent(surl[1]), id[0])
	} else if len(id) == 0 {
		u = fmt.Sprintf("%s/%s", surl[0], encodeURIComponent(surl[1]))
	} else {
		return nil, nil, errors.New("wrong params")
	}

	var res ObjectCopyResult
	copyOpt := &ObjectCopyOptions{
		&ObjectCopyHeaderOptions{},
		&ACLHeaderOptions{},
	}
	if opt != nil {
		if opt.ObjectCopyHeaderOptions != nil {
			*copyOpt.ObjectCopyHeaderOptions = *opt.ObjectCopyHeaderOptions
		}
		if opt.ACLHeaderOptions != nil {
			*copyOpt.ACLHeaderOptions = *opt.ACLHeaderOptions
		}
	}
	copyOpt.XCosCopySource = u

	sendOpt := sendOptions{
		baseURL:   s.client.BaseURL.BucketURL,
		uri:       "/" + encodeURIComponent(name),
		method:    http.MethodPut,
		body:      nil,
		optHeader: copyOpt,
		result:    &res,
	}
	resp, err := s.client.doRetry(ctx, &sendOpt)
	// If the error occurs during the copy operation, the error response is embedded in the 200 OK response. This means that a 200 OK response can contain either a success or an error.
	if err == nil && resp.StatusCode == 200 {
		if res.ETag == "" {
			return &res, resp, errors.New("response 200 OK, but body contains an error")
		}
	}
	return &res, resp, err
}

type ObjectDeleteOptions struct {
	// SSE-C
	XCosSSECustomerAglo   string `header:"x-cos-server-side-encryption-customer-algorithm,omitempty" url:"-" xml:"-"`
	XCosSSECustomerKey    string `header:"x-cos-server-side-encryption-customer-key,omitempty" url:"-" xml:"-"`
	XCosSSECustomerKeyMD5 string `header:"x-cos-server-side-encryption-customer-key-MD5,omitempty" url:"-" xml:"-"`
	//兼容其他自定义头部
	XOptionHeader *http.Header `header:"-,omitempty" url:"-" xml:"-"`
	VersionId     string       `header:"-" url:"VersionId,omitempty" xml:"-"`
}

// Delete Object请求可以将一个文件（Object）删除。
//
// https://www.qcloud.com/document/product/436/7743
func (s *ObjectService) Delete(ctx context.Context, name string, opt ...*ObjectDeleteOptions) (*Response, error) {
	var optHeader *ObjectDeleteOptions
	// When use "" string might call the delete bucket interface
	if len(name) == 0 {
		return nil, errors.New("empty object name")
	}
	if len(opt) > 0 {
		optHeader = opt[0]
	}

	sendOpt := sendOptions{
		baseURL:   s.client.BaseURL.BucketURL,
		uri:       "/" + encodeURIComponent(name),
		method:    http.MethodDelete,
		optHeader: optHeader,
		optQuery:  optHeader,
	}
	resp, err := s.client.doRetry(ctx, &sendOpt)
	return resp, err
}

// ObjectHeadOptions is the option of HeadObject
type ObjectHeadOptions struct {
	IfModifiedSince string `url:"-" header:"If-Modified-Since,omitempty"`
	// SSE-C
	XCosSSECustomerAglo   string       `header:"x-cos-server-side-encryption-customer-algorithm,omitempty" url:"-" xml:"-"`
	XCosSSECustomerKey    string       `header:"x-cos-server-side-encryption-customer-key,omitempty" url:"-" xml:"-"`
	XCosSSECustomerKeyMD5 string       `header:"x-cos-server-side-encryption-customer-key-MD5,omitempty" url:"-" xml:"-"`
	XOptionHeader         *http.Header `header:"-,omitempty" url:"-" xml:"-"`
}

// Head Object请求可以取回对应Object的元数据，Head的权限与Get的权限一致
//
// https://www.qcloud.com/document/product/436/7745
func (s *ObjectService) Head(ctx context.Context, name string, opt *ObjectHeadOptions, id ...string) (*Response, error) {
	var u string
	if len(id) == 1 {
		u = fmt.Sprintf("/%s?versionId=%s", encodeURIComponent(name), id[0])
	} else if len(id) == 0 {
		u = "/" + encodeURIComponent(name)
	} else {
		return nil, errors.New("wrong params")
	}

	sendOpt := sendOptions{
		baseURL:   s.client.BaseURL.BucketURL,
		uri:       u,
		method:    http.MethodHead,
		optHeader: opt,
	}
	resp, err := s.client.doRetry(ctx, &sendOpt)
	if resp != nil && resp.Header["X-Cos-Object-Type"] != nil && resp.Header["X-Cos-Object-Type"][0] == "appendable" {
		resp.Header.Add("x-cos-next-append-position", resp.Header["Content-Length"][0])
	}

	return resp, err
}

// ObjectOptionsOptions is the option of object options
type ObjectOptionsOptions struct {
	Origin                      string `url:"-" header:"Origin"`
	AccessControlRequestMethod  string `url:"-" header:"Access-Control-Request-Method"`
	AccessControlRequestHeaders string `url:"-" header:"Access-Control-Request-Headers,omitempty"`
}

// Options Object请求实现跨域访问的预请求。即发出一个 OPTIONS 请求给服务器以确认是否可以进行跨域操作。
//
// 当CORS配置不存在时，请求返回403 Forbidden。
//
// https://www.qcloud.com/document/product/436/8288
func (s *ObjectService) Options(ctx context.Context, name string, opt *ObjectOptionsOptions) (*Response, error) {
	sendOpt := sendOptions{
		baseURL:   s.client.BaseURL.BucketURL,
		uri:       "/" + encodeURIComponent(name),
		method:    http.MethodOptions,
		optHeader: opt,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return resp, err
}

// CASJobParameters support three way: Standard(in 35 hours), Expedited(quick way, in 15 mins), Bulk(in 5-12 hours_
type CASJobParameters struct {
	Tier string `xml:"Tier" header:"-" url:"-"`
}

// ObjectRestoreOptions is the option of object restore
type ObjectRestoreOptions struct {
	XMLName       xml.Name          `xml:"RestoreRequest" header:"-" url:"-"`
	Days          int               `xml:"Days" header:"-" url:"-"`
	Tier          *CASJobParameters `xml:"CASJobParameters" header:"-" url:"-"`
	XOptionHeader *http.Header      `xml:"-" header:",omitempty" url:"-"`
}

// PutRestore API can recover an object of type archived by COS archive.
//
// https://cloud.tencent.com/document/product/436/12633
func (s *ObjectService) PostRestore(ctx context.Context, name string, opt *ObjectRestoreOptions) (*Response, error) {
	u := fmt.Sprintf("/%s?restore", encodeURIComponent(name))
	sendOpt := sendOptions{
		baseURL:   s.client.BaseURL.BucketURL,
		uri:       u,
		method:    http.MethodPost,
		body:      opt,
		optHeader: opt,
	}
	resp, err := s.client.doRetry(ctx, &sendOpt)

	return resp, err
}

// Append请求可以将一个文件（Object）以分块追加的方式上传至 Bucket 中。使用Append Upload的文件必须事前被设定为Appendable。
// 当Appendable的文件被执行Put Object的操作以后，文件被覆盖，属性改变为Normal。
//
// 文件属性可以在Head Object操作中被查询到，当您发起Head Object请求时，会返回自定义Header『x-cos-object-type』，该Header只有两个枚举值：Normal或者Appendable。
//
// 追加上传建议文件大小1M - 5G。如果position的值和当前Object的长度不致，COS会返回409错误。
// 如果Append一个Normal的Object，COS会返回409 ObjectNotAppendable。
//
// Appendable的文件不可以被复制，不参与版本管理，不参与生命周期管理，不可跨区域复制。
//
// 当 r 不是 bytes.Buffer/bytes.Reader/strings.Reader 时，必须指定 opt.ObjectPutHeaderOptions.ContentLength
//
// https://www.qcloud.com/document/product/436/7741
func (s *ObjectService) Append(ctx context.Context, name string, position int, r io.Reader, opt *ObjectPutOptions) (int, *Response, error) {
	res := position
	if r == nil {
		return res, nil, fmt.Errorf("reader is nil")
	}
	if err := CheckReaderLen(r); err != nil {
		return res, nil, err
	}
	opt = CloneObjectPutOptions(opt)
	totalBytes, err := GetReaderLen(r)
	if err != nil && opt != nil && opt.Listener != nil {
		if opt.ContentLength == 0 {
			return res, nil, err
		}
		totalBytes = opt.ContentLength
	}
	if err == nil {
		// 与 go http 保持一致, 非bytes.Buffer/bytes.Reader/strings.Reader需用户指定ContentLength
		if opt != nil && opt.ContentLength == 0 && IsLenReader(r) {
			opt.ContentLength = totalBytes
		}
	}
	reader := TeeReader(r, nil, totalBytes, nil)
	if s.client.Conf.EnableCRC {
		reader.writer = md5.New() // MD5校验
		reader.disableCheckSum = true
	}
	if opt != nil && opt.Listener != nil {
		reader.listener = opt.Listener
	}
	u := fmt.Sprintf("/%s?append&position=%d", encodeURIComponent(name), position)
	sendOpt := sendOptions{
		baseURL:   s.client.BaseURL.BucketURL,
		uri:       u,
		method:    http.MethodPost,
		optHeader: opt,
		body:      reader,
	}
	resp, err := s.client.send(ctx, &sendOpt)

	if err == nil {
		// 数据校验
		if s.client.Conf.EnableCRC && reader.writer != nil {
			wanted := hex.EncodeToString(reader.Sum())
			if wanted != resp.Header.Get("x-cos-content-sha1") {
				return res, resp, fmt.Errorf("append verification failed, want:%v, return:%v", wanted, resp.Header.Get("x-cos-content-sha1"))
			}
		}
		np, err := strconv.ParseInt(resp.Header.Get("x-cos-next-append-position"), 10, 64)
		return int(np), resp, err
	}
	return res, resp, err
}

// ObjectDeleteMultiOptions is the option of DeleteMulti
type ObjectDeleteMultiOptions struct {
	XMLName xml.Name `xml:"Delete" header:"-"`
	Quiet   bool     `xml:"Quiet" header:"-"`
	Objects []Object `xml:"Object" header:"-"`
	//XCosSha1 string `xml:"-" header:"x-cos-sha1"`
}

// ObjectDeleteMultiResult is the result of DeleteMulti
type ObjectDeleteMultiResult struct {
	XMLName        xml.Name `xml:"DeleteResult"`
	DeletedObjects []Object `xml:"Deleted,omitempty"`
	Errors         []struct {
		Key       string `xml:",omitempty"`
		Code      string `xml:",omitempty"`
		Message   string `xml:",omitempty"`
		VersionId string `xml:",omitempty"`
	} `xml:"Error,omitempty"`
}

// DeleteMulti 请求实现批量删除文件，最大支持单次删除1000个文件。
// 对于返回结果，COS提供Verbose和Quiet两种结果模式。Verbose模式将返回每个Object的删除结果；
// Quiet模式只返回报错的Object信息。
// https://www.qcloud.com/document/product/436/8289
func (s *ObjectService) DeleteMulti(ctx context.Context, opt *ObjectDeleteMultiOptions) (*ObjectDeleteMultiResult, *Response, error) {
	var res ObjectDeleteMultiResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     "/?delete",
		method:  http.MethodPost,
		body:    opt,
		result:  &res,
	}
	resp, err := s.client.doRetry(ctx, &sendOpt)
	return &res, resp, err
}

// Object is the meta info of the object
type Object struct {
	Key          string `xml:",omitempty"`
	ETag         string `xml:",omitempty"`
	Size         int64  `xml:",omitempty"`
	PartNumber   int    `xml:",omitempty"`
	LastModified string `xml:",omitempty"`
	StorageClass string `xml:",omitempty"`
	Owner        *Owner `xml:",omitempty"`
	VersionId    string `xml:",omitempty"`
}

// MultiUploadOptions is the option of the multiupload,
// ThreadPoolSize default is one
type MultiUploadOptions struct {
	OptIni          *InitiateMultipartUploadOptions
	PartSize        int64
	ThreadPoolSize  int
	CheckPoint      bool
	DisableChecksum bool
}

type MultiDownloadOptions struct {
	Opt             *ObjectGetOptions
	PartSize        int64
	ThreadPoolSize  int
	CheckPoint      bool
	CheckPointFile  string
	DisableChecksum bool
}

type MultiDownloadCPInfo struct {
	Size             int64             `json:"contentLength,omitempty"`
	ETag             string            `json:"eTag,omitempty"`
	CRC64            string            `json:"crc64ecma,omitempty"`
	LastModified     string            `json:"lastModified,omitempty"`
	DownloadedBlocks []DownloadedBlock `json:"downloadedBlocks,omitempty"`
}
type DownloadedBlock struct {
	From int64 `json:"from,omitempty"`
	To   int64 `json:"to,omitempty"`
}

type Chunk struct {
	Number int
	OffSet int64
	Size   int64
	Done   bool
	ETag   string
}

// jobs
type Jobs struct {
	Name       string
	UploadId   string
	FilePath   string
	RetryTimes int
	VersionId  []string
	Chunk      Chunk
	Data       io.Reader
	Opt        *ObjectUploadPartOptions
	DownOpt    *ObjectGetOptions
}

type Results struct {
	PartNumber int
	Resp       *Response
	err        error
}

func LimitReadCloser(r io.Reader, n int64) io.Reader {
	var lc LimitedReadCloser
	lc.R = r
	lc.N = n
	return &lc
}

type LimitedReadCloser struct {
	io.LimitedReader
}

func (lc *LimitedReadCloser) Close() error {
	if r, ok := lc.R.(io.ReadCloser); ok {
		return r.Close()
	}
	return nil
}

type DiscardReadCloser struct {
	RC      io.ReadCloser
	Discard int
}

func (drc *DiscardReadCloser) Read(data []byte) (int, error) {
	n, err := drc.RC.Read(data)
	if drc.Discard == 0 || n <= 0 {
		return n, err
	}

	if n <= drc.Discard {
		drc.Discard -= n
		return 0, err
	}

	realLen := n - drc.Discard
	copy(data[0:realLen], data[drc.Discard:n])
	drc.Discard = 0
	return realLen, err
}

func (drc *DiscardReadCloser) Close() error {
	if rc, ok := drc.RC.(io.ReadCloser); ok {
		return rc.Close()
	}
	return nil
}

func worker(ctx context.Context, s *ObjectService, jobs <-chan *Jobs, results chan<- *Results) {
	for j := range jobs {
		j.Opt.ContentLength = j.Chunk.Size

		rt := j.RetryTimes
		for {
			// http.Request.Body can be Closed in request
			fd, err := os.Open(j.FilePath)
			var res Results
			if err != nil {
				res.err = err
				res.PartNumber = j.Chunk.Number
				res.Resp = nil
				results <- &res
				break
			}
			fd.Seek(j.Chunk.OffSet, os.SEEK_SET)
			resp, err := s.UploadPart(ctx, j.Name, j.UploadId, j.Chunk.Number,
				LimitReadCloser(fd, j.Chunk.Size), j.Opt)
			res.PartNumber = j.Chunk.Number
			res.Resp = resp
			res.err = err
			if err != nil {
				rt--
				if rt == 0 {
					results <- &res
					break
				}
				time.Sleep(time.Millisecond)
				continue
			}
			results <- &res
			break
		}
	}
}

func downloadWorker(ctx context.Context, s *ObjectService, jobs <-chan *Jobs, results chan<- *Results) {
	for j := range jobs {
		opt := &RangeOptions{
			HasStart: true,
			HasEnd:   true,
			Start:    j.Chunk.OffSet,
			End:      j.Chunk.OffSet + j.Chunk.Size - 1,
		}
		j.DownOpt.Range = FormatRangeOptions(opt)
		rt := j.RetryTimes
		for {
			var res Results
			res.PartNumber = j.Chunk.Number
			resp, err := s.Get(ctx, j.Name, j.DownOpt, j.VersionId...)
			res.err = err
			res.Resp = resp
			if err != nil {
				results <- &res
				break
			}
			fd, err := os.OpenFile(j.FilePath, os.O_WRONLY, 0660)
			if err != nil {
				resp.Body.Close()
				res.err = err
				results <- &res
				break
			}
			fd.Seek(j.Chunk.OffSet, os.SEEK_SET)
			n, err := io.Copy(fd, LimitReadCloser(resp.Body, j.Chunk.Size))
			if n != j.Chunk.Size || err != nil {
				fd.Close()
				resp.Body.Close()
				rt--
				if rt == 0 {
					res.err = fmt.Errorf("io.Copy Failed, nread:%v, want:%v, err:%v", n, j.Chunk.Size, err)
					results <- &res
					break
				}
				time.Sleep(time.Millisecond)
				continue
			}
			fd.Close()
			resp.Body.Close()
			results <- &res
			break
		}
	}
}

func DividePart(fileSize int64, last int) (int64, int64) {
	partSize := int64(last * 1024 * 1024)
	partNum := fileSize / partSize
	for partNum >= 10000 {
		partSize = partSize * 2
		partNum = fileSize / partSize
	}
	return partNum, partSize
}

func SplitFileIntoChunks(filePath string, partSize int64) (int64, []Chunk, int, error) {
	if filePath == "" {
		return 0, nil, 0, errors.New("filePath invalid")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return 0, nil, 0, err
	}
	defer file.Close()

	stat, err := file.Stat()
	if err != nil {
		return 0, nil, 0, err
	}
	var partNum int64
	if partSize > 0 {
		if partSize < 1024*1024 {
			return 0, nil, 0, errors.New("partSize>=1048576 is required")
		}
		partNum = stat.Size() / partSize
		if partNum >= 10000 {
			return 0, nil, 0, errors.New("Too many parts, out of 10000")
		}
	} else {
		partNum, partSize = DividePart(stat.Size(), 16)
	}

	var chunks []Chunk
	var chunk = Chunk{}
	for i := int64(0); i < partNum; i++ {
		chunk.Number = int(i + 1)
		chunk.OffSet = i * partSize
		chunk.Size = partSize
		chunks = append(chunks, chunk)
	}

	if stat.Size()%partSize > 0 {
		chunk.Number = len(chunks) + 1
		chunk.OffSet = int64(len(chunks)) * partSize
		chunk.Size = stat.Size() % partSize
		chunks = append(chunks, chunk)
		partNum++
	}

	return int64(stat.Size()), chunks, int(partNum), nil

}

func (s *ObjectService) getResumableUploadID(ctx context.Context, name string) (string, error) {
	opt := &ObjectListUploadsOptions{
		Prefix:       name,
		EncodingType: "url",
	}
	res, _, err := s.ListUploads(ctx, opt)
	if err != nil {
		return "", err
	}
	if len(res.Upload) == 0 {
		return "", nil
	}
	last := len(res.Upload) - 1
	for last >= 0 {
		decodeKey, _ := decodeURIComponent(res.Upload[last].Key)
		if decodeKey == name {
			return decodeURIComponent(res.Upload[last].UploadID)
		}
		last = last - 1
	}
	return "", nil
}

func (s *ObjectService) checkUploadedParts(ctx context.Context, name, UploadID, filepath string, chunks []Chunk, partNum int) error {
	var uploadedParts []Object
	isTruncated := true
	opt := &ObjectListPartsOptions{
		EncodingType: "url",
	}
	for isTruncated {
		res, _, err := s.ListParts(ctx, name, UploadID, opt)
		if err != nil {
			return err
		}
		if len(res.Parts) > 0 {
			uploadedParts = append(uploadedParts, res.Parts...)
		}
		isTruncated = res.IsTruncated
		opt.PartNumberMarker = res.NextPartNumberMarker
	}
	fd, err := os.Open(filepath)
	if err != nil {
		return err
	}
	defer fd.Close()
	// 某个分块出错, 重置chunks
	ret := func(e error) error {
		for i, _ := range chunks {
			chunks[i].Done = false
			chunks[i].ETag = ""
		}
		return e
	}
	for _, part := range uploadedParts {
		partNumber := part.PartNumber
		if partNumber > partNum {
			return ret(errors.New("Part Number is not consistent"))
		}
		partNumber = partNumber - 1
		fd.Seek(chunks[partNumber].OffSet, os.SEEK_SET)
		bs, err := ioutil.ReadAll(io.LimitReader(fd, chunks[partNumber].Size))
		if err != nil {
			return ret(err)
		}
		localMD5 := fmt.Sprintf("\"%x\"", md5.Sum(bs))
		if localMD5 != part.ETag {
			return ret(errors.New(fmt.Sprintf("CheckSum Failed in Part[%d]", part.PartNumber)))
		}
		chunks[partNumber].Done = true
		chunks[partNumber].ETag = part.ETag
	}
	return nil
}

// MultiUpload/Upload 为高级upload接口，并发分块上传
//
// 当 partSize > 0 时，由调用者指定分块大小，否则由 SDK 自动切分，单位为MB
// 由调用者指定分块大小时，请确认分块数量不超过10000
//
func (s *ObjectService) MultiUpload(ctx context.Context, name string, filepath string, opt *MultiUploadOptions) (*CompleteMultipartUploadResult, *Response, error) {
	return s.Upload(ctx, name, filepath, opt)
}

func (s *ObjectService) Upload(ctx context.Context, name string, filepath string, opt *MultiUploadOptions) (*CompleteMultipartUploadResult, *Response, error) {
	if opt == nil {
		opt = &MultiUploadOptions{}
	}
	var localcrc uint64
	// 1.Get the file chunk
	totalBytes, chunks, partNum, err := SplitFileIntoChunks(filepath, opt.PartSize*1024*1024)
	if err != nil {
		return nil, nil, err
	}
	// 校验
	if s.client.Conf.EnableCRC && !opt.DisableChecksum {
		fd, err := os.Open(filepath)
		if err != nil {
			return nil, nil, err
		}
		defer fd.Close()
		localcrc, err = calCRC64(fd)
		if err != nil {
			return nil, nil, err
		}
	}
	// filesize=0 , use simple upload
	if partNum == 0 || partNum == 1 {
		var opt0 *ObjectPutOptions
		if opt.OptIni != nil {
			opt0 = &ObjectPutOptions{
				opt.OptIni.ACLHeaderOptions,
				opt.OptIni.ObjectPutHeaderOptions,
			}
		}
		rsp, err := s.PutFromFile(ctx, name, filepath, opt0)
		if err != nil {
			return nil, rsp, err
		}
		result := &CompleteMultipartUploadResult{
			Location: fmt.Sprintf("%s/%s", s.client.BaseURL.BucketURL, name),
			Key:      name,
			ETag:     rsp.Header.Get("ETag"),
		}
		if rsp != nil && s.client.Conf.EnableCRC && !opt.DisableChecksum {
			scoscrc := rsp.Header.Get("x-cos-hash-crc64ecma")
			icoscrc, _ := strconv.ParseUint(scoscrc, 10, 64)
			if icoscrc != localcrc {
				return result, rsp, fmt.Errorf("verification failed, want:%v, return:%v", localcrc, icoscrc)
			}
		}
		return result, rsp, nil
	}

	var uploadID string
	resumableFlag := false
	if opt.CheckPoint {
		var err error
		uploadID, err = s.getResumableUploadID(ctx, name)
		if err == nil && uploadID != "" {
			err = s.checkUploadedParts(ctx, name, uploadID, filepath, chunks, partNum)
			resumableFlag = (err == nil)
		}
	}

	// 2.Init
	optini := opt.OptIni
	if !resumableFlag {
		res, _, err := s.InitiateMultipartUpload(ctx, name, optini)
		if err != nil {
			return nil, nil, err
		}
		uploadID = res.UploadID
	}
	var poolSize int
	if opt.ThreadPoolSize > 0 {
		poolSize = opt.ThreadPoolSize
	} else {
		// Default is one
		poolSize = 1
	}

	chjobs := make(chan *Jobs, 100)
	chresults := make(chan *Results, 10000)
	optcom := &CompleteMultipartUploadOptions{}

	// 3.Start worker
	for w := 1; w <= poolSize; w++ {
		go worker(ctx, s, chjobs, chresults)
	}

	// progress started event
	var listener ProgressListener
	var consumedBytes int64
	if opt.OptIni != nil {
		if opt.OptIni.ObjectPutHeaderOptions != nil {
			listener = opt.OptIni.Listener
		}
		optcom.XOptionHeader, _ = deliverInitOptions(opt.OptIni)
	}
	event := newProgressEvent(ProgressStartedEvent, 0, 0, totalBytes)
	progressCallback(listener, event)

	// 4.Push jobs
	go func() {
		for _, chunk := range chunks {
			if chunk.Done {
				continue
			}
			partOpt := &ObjectUploadPartOptions{}
			if optini != nil && optini.ObjectPutHeaderOptions != nil {
				partOpt.XCosSSECustomerAglo = optini.XCosSSECustomerAglo
				partOpt.XCosSSECustomerKey = optini.XCosSSECustomerKey
				partOpt.XCosSSECustomerKeyMD5 = optini.XCosSSECustomerKeyMD5
				partOpt.XCosTrafficLimit = optini.XCosTrafficLimit
			}
			job := &Jobs{
				Name:       name,
				RetryTimes: 3,
				FilePath:   filepath,
				UploadId:   uploadID,
				Chunk:      chunk,
				Opt:        partOpt,
			}
			chjobs <- job
		}
		close(chjobs)
	}()

	// 5.Recv the resp etag to complete
	err = nil
	for i := 0; i < partNum; i++ {
		if chunks[i].Done {
			optcom.Parts = append(optcom.Parts, Object{
				PartNumber: chunks[i].Number, ETag: chunks[i].ETag},
			)
			if err == nil {
				consumedBytes += chunks[i].Size
				event = newProgressEvent(ProgressDataEvent, chunks[i].Size, consumedBytes, totalBytes)
				progressCallback(listener, event)
			}
			continue
		}
		res := <-chresults
		// Notice one part fail can not get the etag according.
		if res.Resp == nil || res.err != nil {
			// Some part already fail, can not to get the header inside.
			err = fmt.Errorf("UploadID %s, part %d failed to get resp content. error: %s", uploadID, res.PartNumber, res.err.Error())
			continue
		}
		// Notice one part fail can not get the etag according.
		etag := res.Resp.Header.Get("ETag")
		optcom.Parts = append(optcom.Parts, Object{
			PartNumber: res.PartNumber, ETag: etag},
		)
		if err == nil {
			consumedBytes += chunks[res.PartNumber-1].Size
			event = newProgressEvent(ProgressDataEvent, chunks[res.PartNumber-1].Size, consumedBytes, totalBytes)
			progressCallback(listener, event)
		}
	}
	close(chresults)
	if err != nil {
		event = newProgressEvent(ProgressFailedEvent, 0, consumedBytes, totalBytes, err)
		progressCallback(listener, event)
		return nil, nil, err
	}
	sort.Sort(ObjectList(optcom.Parts))

	event = newProgressEvent(ProgressCompletedEvent, 0, consumedBytes, totalBytes)
	progressCallback(listener, event)

	v, resp, err := s.CompleteMultipartUpload(context.Background(), name, uploadID, optcom)
	if err != nil {
		return v, resp, err
	}

	if resp != nil && s.client.Conf.EnableCRC && !opt.DisableChecksum {
		scoscrc := resp.Header.Get("x-cos-hash-crc64ecma")
		icoscrc, _ := strconv.ParseUint(scoscrc, 10, 64)
		if icoscrc != localcrc {
			return v, resp, fmt.Errorf("verification failed, want:%v, return:%v", localcrc, icoscrc)
		}
	}
	return v, resp, err
}

func SplitSizeIntoChunks(totalBytes int64, partSize int64) ([]Chunk, int, error) {
	var partNum int64
	if partSize > 0 {
		if partSize < 1024*1024 {
			return nil, 0, errors.New("partSize>=1048576 is required")
		}
		partNum = totalBytes / partSize
		if partNum >= 10000 {
			return nil, 0, errors.New("Too manry parts, out of 10000")
		}
	} else {
		partNum, partSize = DividePart(totalBytes, 16)
	}

	var chunks []Chunk
	var chunk = Chunk{}
	for i := int64(0); i < partNum; i++ {
		chunk.Number = int(i + 1)
		chunk.OffSet = i * partSize
		chunk.Size = partSize
		chunks = append(chunks, chunk)
	}

	if totalBytes%partSize > 0 {
		chunk.Number = len(chunks) + 1
		chunk.OffSet = int64(len(chunks)) * partSize
		chunk.Size = totalBytes % partSize
		chunks = append(chunks, chunk)
		partNum++
	}

	return chunks, int(partNum), nil
}

func (s *ObjectService) checkDownloadedParts(opt *MultiDownloadCPInfo, chfile string, chunks []Chunk) (*MultiDownloadCPInfo, bool) {
	var defaultRes MultiDownloadCPInfo
	defaultRes = *opt

	fd, err := os.Open(chfile)
	// checkpoint 文件不存在
	if err != nil && os.IsNotExist(err) {
		// 创建 checkpoint 文件
		fd, _ = os.OpenFile(chfile, os.O_RDONLY|os.O_CREATE|os.O_TRUNC, 0660)
		fd.Close()
		return &defaultRes, false
	}
	if err != nil {
		return &defaultRes, false
	}
	defer fd.Close()

	var res MultiDownloadCPInfo
	err = json.NewDecoder(fd).Decode(&res)
	if err != nil {
		return &defaultRes, false
	}
	// 与COS的文件比较
	if res.CRC64 != opt.CRC64 || res.ETag != opt.ETag || res.Size != opt.Size || res.LastModified != opt.LastModified || len(res.DownloadedBlocks) == 0 {
		return &defaultRes, false
	}
	// len(chunks) 大于1，否则为简单下载, chunks[0].Size为partSize
	partSize := chunks[0].Size
	for _, v := range res.DownloadedBlocks {
		index := v.From / partSize
		to := chunks[index].OffSet + chunks[index].Size - 1
		if chunks[index].OffSet != v.From || to != v.To {
			// 重置chunks
			for i, _ := range chunks {
				chunks[i].Done = false
			}
			return &defaultRes, false
		}
		chunks[index].Done = true
	}
	return &res, true
}

func (s *ObjectService) Download(ctx context.Context, name string, filepath string, opt *MultiDownloadOptions, id ...string) (*Response, error) {
	// 参数校验
	if opt == nil {
		opt = &MultiDownloadOptions{}
	}
	if opt.Opt != nil && opt.Opt.Range != "" {
		return nil, fmt.Errorf("Download doesn't support Range Options")
	}
	// 获取文件长度和CRC
	var coscrc string
	resp, err := s.Head(ctx, name, nil, id...)
	if err != nil {
		return resp, err
	}
	// 如果对象不存在x-cos-hash-crc64ecma，则跳过不做校验
	coscrc = resp.Header.Get("x-cos-hash-crc64ecma")
	strTotalBytes := resp.Header.Get("Content-Length")
	totalBytes, err := strconv.ParseInt(strTotalBytes, 10, 64)
	if err != nil {
		return resp, err
	}

	// 切分
	chunks, partNum, err := SplitSizeIntoChunks(totalBytes, opt.PartSize*1024*1024)
	if err != nil {
		return resp, err
	}
	// 直接下载到文件
	if partNum == 0 || partNum == 1 {
		rsp, err := s.GetToFile(ctx, name, filepath, opt.Opt, id...)
		if err != nil {
			return rsp, err
		}
		if coscrc != "" && s.client.Conf.EnableCRC && !opt.DisableChecksum {
			icoscrc, _ := strconv.ParseUint(coscrc, 10, 64)
			fd, err := os.Open(filepath)
			if err != nil {
				return rsp, err
			}
			defer fd.Close()
			localcrc, err := calCRC64(fd)
			if err != nil {
				return rsp, err
			}
			if localcrc != icoscrc {
				return rsp, fmt.Errorf("verification failed, want:%v, return:%v", icoscrc, localcrc)
			}
		}
		return rsp, err
	}
	// 断点续载
	var resumableFlag bool
	var resumableInfo *MultiDownloadCPInfo
	var cpfd *os.File
	var cpfile string
	if opt.CheckPoint {
		cpInfo := &MultiDownloadCPInfo{
			LastModified: resp.Header.Get("Last-Modified"),
			ETag:         resp.Header.Get("ETag"),
			CRC64:        coscrc,
			Size:         totalBytes,
		}
		cpfile = opt.CheckPointFile
		if cpfile == "" {
			cpfile = fmt.Sprintf("%s.cosresumabletask", filepath)
		}
		resumableInfo, resumableFlag = s.checkDownloadedParts(cpInfo, cpfile, chunks)
		cpfd, err = os.OpenFile(cpfile, os.O_RDWR, 0660)
		if err != nil {
			return nil, fmt.Errorf("Open CheckPoint File[%v] Failed:%v", cpfile, err)
		}
	}
	if !resumableFlag {
		// 创建文件
		nfile, err := os.OpenFile(filepath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0660)
		if err != nil {
			if cpfd != nil {
				cpfd.Close()
			}
			return resp, err
		}
		nfile.Close()
	}

	var poolSize int
	if opt.ThreadPoolSize > 0 {
		poolSize = opt.ThreadPoolSize
	} else {
		poolSize = 1
	}
	chjobs := make(chan *Jobs, 100)
	chresults := make(chan *Results, 10000)
	for w := 1; w <= poolSize; w++ {
		go downloadWorker(ctx, s, chjobs, chresults)
	}

	go func() {
		for _, chunk := range chunks {
			if chunk.Done {
				continue
			}
			var downOpt ObjectGetOptions
			if opt.Opt != nil {
				downOpt = *opt.Opt
				downOpt.Listener = nil // listener need to set nil
			}
			job := &Jobs{
				Name:       name,
				RetryTimes: 3,
				FilePath:   filepath,
				Chunk:      chunk,
				DownOpt:    &downOpt,
			}
			if len(id) > 0 {
				job.VersionId = append(job.VersionId, id...)
			}
			chjobs <- job
		}
		close(chjobs)
	}()
	err = nil
	for i := 0; i < partNum; i++ {
		if chunks[i].Done {
			continue
		}
		res := <-chresults
		if res.Resp == nil || res.err != nil {
			err = fmt.Errorf("part %d get resp Content. error: %s", res.PartNumber, res.err.Error())
			continue
		}
		// Dump CheckPoint Info
		if opt.CheckPoint {
			cpfd.Truncate(0)
			cpfd.Seek(0, os.SEEK_SET)
			resumableInfo.DownloadedBlocks = append(resumableInfo.DownloadedBlocks, DownloadedBlock{
				From: chunks[res.PartNumber-1].OffSet,
				To:   chunks[res.PartNumber-1].OffSet + chunks[res.PartNumber-1].Size - 1,
			})
			json.NewEncoder(cpfd).Encode(resumableInfo)
		}
	}
	close(chresults)
	if cpfd != nil {
		cpfd.Close()
	}
	if err != nil {
		return nil, err
	}
	// 下载成功，删除checkpoint文件
	if opt.CheckPoint {
		os.Remove(cpfile)
	}
	if coscrc != "" && s.client.Conf.EnableCRC && !opt.DisableChecksum {
		icoscrc, _ := strconv.ParseUint(coscrc, 10, 64)
		fd, err := os.Open(filepath)
		if err != nil {
			return resp, err
		}
		defer fd.Close()
		localcrc, err := calCRC64(fd)
		if err != nil {
			return resp, err
		}
		if localcrc != icoscrc {
			return resp, fmt.Errorf("verification failed, want:%v, return:%v", icoscrc, localcrc)
		}
	}
	return resp, err
}

type ObjectPutTaggingOptions struct {
	XMLName xml.Name           `xml:"Tagging"`
	TagSet  []ObjectTaggingTag `xml:"TagSet>Tag,omitempty"`
}
type ObjectTaggingTag BucketTaggingTag
type ObjectGetTaggingResult ObjectPutTaggingOptions

func (s *ObjectService) PutTagging(ctx context.Context, name string, opt *ObjectPutTaggingOptions, id ...string) (*Response, error) {
	var u string
	if len(id) == 1 {
		u = fmt.Sprintf("/%s?tagging&versionId=%s", encodeURIComponent(name), id[0])
	} else if len(id) == 0 {
		u = fmt.Sprintf("/%s?tagging", encodeURIComponent(name))
	} else {
		return nil, errors.New("wrong params")
	}
	sendOpt := &sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     u,
		method:  http.MethodPut,
		body:    opt,
	}
	resp, err := s.client.doRetry(ctx, sendOpt)
	return resp, err
}

func (s *ObjectService) GetTagging(ctx context.Context, name string, id ...string) (*ObjectGetTaggingResult, *Response, error) {
	var u string
	if len(id) == 1 {
		u = fmt.Sprintf("/%s?tagging&versionId=%s", encodeURIComponent(name), id[0])
	} else if len(id) == 0 {
		u = fmt.Sprintf("/%s?tagging", encodeURIComponent(name))
	} else {
		return nil, nil, errors.New("wrong params")
	}

	var res ObjectGetTaggingResult
	sendOpt := &sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     u,
		method:  http.MethodGet,
		result:  &res,
	}
	resp, err := s.client.doRetry(ctx, sendOpt)
	return &res, resp, err
}

func (s *ObjectService) DeleteTagging(ctx context.Context, name string, id ...string) (*Response, error) {
	var u string
	if len(id) == 1 {
		u = fmt.Sprintf("/%s?tagging&versionId=%s", encodeURIComponent(name), id[0])
	} else if len(id) == 0 {
		u = fmt.Sprintf("/%s?tagging", encodeURIComponent(name))
	} else {
		return nil, errors.New("wrong params")
	}

	sendOpt := &sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     u,
		method:  http.MethodDelete,
	}
	resp, err := s.client.doRetry(ctx, sendOpt)
	return resp, err
}

package cos

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/crc64"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// InitiateMultipartUploadOptions is the option of InitateMultipartUpload
type InitiateMultipartUploadOptions struct {
	*ACLHeaderOptions
	*ObjectPutHeaderOptions
}

// InitiateMultipartUploadResult is the result of InitateMultipartUpload
type InitiateMultipartUploadResult struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	Bucket   string
	Key      string
	UploadID string `xml:"UploadId"`
}

// InitiateMultipartUpload 请求实现初始化分片上传，成功执行此请求以后会返回Upload ID用于后续的Upload Part请求。
//
// https://www.qcloud.com/document/product/436/7746
func (s *ObjectService) InitiateMultipartUpload(ctx context.Context, name string, opt *InitiateMultipartUploadOptions) (*InitiateMultipartUploadResult, *Response, error) {
	var res InitiateMultipartUploadResult
	sendOpt := sendOptions{
		baseURL:   s.client.BaseURL.BucketURL,
		uri:       "/" + encodeURIComponent(name) + "?uploads",
		method:    http.MethodPost,
		optHeader: opt,
		result:    &res,
	}
	resp, err := s.client.doRetry(ctx, &sendOpt)
	return &res, resp, err
}

// ObjectUploadPartOptions is the options of upload-part
type ObjectUploadPartOptions struct {
	Expect                string `header:"Expect,omitempty" url:"-"`
	XCosContentSHA1       string `header:"x-cos-content-sha1,omitempty" url:"-"`
	ContentLength         int64  `header:"Content-Length,omitempty" url:"-"`
	ContentMD5            string `header:"Content-MD5,omitempty" url:"-"`
	XCosSSECustomerAglo   string `header:"x-cos-server-side-encryption-customer-algorithm,omitempty" url:"-" xml:"-"`
	XCosSSECustomerKey    string `header:"x-cos-server-side-encryption-customer-key,omitempty" url:"-" xml:"-"`
	XCosSSECustomerKeyMD5 string `header:"x-cos-server-side-encryption-customer-key-MD5,omitempty" url:"-" xml:"-"`

	XCosTrafficLimit int `header:"x-cos-traffic-limit,omitempty" url:"-" xml:"-"`

	XOptionHeader *http.Header `header:"-,omitempty" url:"-" xml:"-"`
	// 上传进度, ProgressCompleteEvent不能表示对应API调用成功，API是否调用成功的判断标准为返回err==nil
	Listener ProgressListener `header:"-" url:"-" xml:"-"`
}

// UploadPart 请求实现在初始化以后的分块上传，支持的块的数量为1到10000，块的大小为1 MB 到5 GB。
// 在每次请求Upload Part时候，需要携带partNumber和uploadID，partNumber为块的编号，支持乱序上传。
//
// 当传入uploadID和partNumber都相同的时候，后传入的块将覆盖之前传入的块。当uploadID不存在时会返回404错误，NoSuchUpload.
//
// 当 r 不是 bytes.Buffer/bytes.Reader/strings.Reader 时，必须指定 opt.ContentLength
//
// https://www.qcloud.com/document/product/436/7750
func (s *ObjectService) UploadPart(ctx context.Context, name, uploadID string, partNumber int, r io.Reader, uopt *ObjectUploadPartOptions) (*Response, error) {
	if r == nil {
		return nil, fmt.Errorf("reader is nil")
	}
	if err := CheckReaderLen(r); err != nil {
		return nil, err
	}
	// opt 不为 nil
	opt := CloneObjectUploadPartOptions(uopt)
	totalBytes, err := GetReaderLen(r)
	if err != nil && opt.Listener != nil {
		if opt.ContentLength == 0 {
			return nil, err
		}
		totalBytes = opt.ContentLength
	}
	// 分块上传不支持 Chunk 上传
	if err == nil {
		// 与 go http 保持一致, 非bytes.Buffer/bytes.Reader/strings.Reader需用户指定ContentLength
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
	u := fmt.Sprintf("/%s?partNumber=%d&uploadId=%s", encodeURIComponent(name), partNumber, uploadID)
	sendOpt := sendOptions{
		baseURL:   s.client.BaseURL.BucketURL,
		uri:       u,
		method:    http.MethodPut,
		optHeader: opt,
		body:      reader,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return resp, err
}

// ObjectListPartsOptions is the option of ListParts
type ObjectListPartsOptions struct {
	EncodingType     string `url:"Encoding-type,omitempty"`
	MaxParts         string `url:"max-parts,omitempty"`
	PartNumberMarker string `url:"part-number-marker,omitempty"`
}

// ObjectListPartsResult is the result of ListParts
type ObjectListPartsResult struct {
	XMLName              xml.Name `xml:"ListPartsResult"`
	Bucket               string
	EncodingType         string `xml:"Encoding-type,omitempty"`
	Key                  string
	UploadID             string     `xml:"UploadId"`
	Initiator            *Initiator `xml:"Initiator,omitempty"`
	Owner                *Owner     `xml:"Owner,omitempty"`
	StorageClass         string
	PartNumberMarker     string
	NextPartNumberMarker string `xml:"NextPartNumberMarker,omitempty"`
	MaxParts             string
	IsTruncated          bool
	Parts                []Object `xml:"Part,omitempty"`
}

// ListParts 用来查询特定分块上传中的已上传的块。
//
// https://www.qcloud.com/document/product/436/7747
func (s *ObjectService) ListParts(ctx context.Context, name, uploadID string, opt *ObjectListPartsOptions) (*ObjectListPartsResult, *Response, error) {
	u := fmt.Sprintf("/%s?uploadId=%s", encodeURIComponent(name), uploadID)
	var res ObjectListPartsResult
	sendOpt := sendOptions{
		baseURL:  s.client.BaseURL.BucketURL,
		uri:      u,
		method:   http.MethodGet,
		result:   &res,
		optQuery: opt,
	}
	resp, err := s.client.doRetry(ctx, &sendOpt)
	return &res, resp, err
}

// CompleteMultipartUploadOptions is the option of CompleteMultipartUpload
type CompleteMultipartUploadOptions struct {
	XMLName       xml.Name     `xml:"CompleteMultipartUpload" header:"-" url:"-"`
	Parts         []Object     `xml:"Part" header:"-" url:"-"`
	XOptionHeader *http.Header `header:"-,omitempty" xml:"-" url:"-"`
}

// CompleteMultipartUploadResult is the result CompleteMultipartUpload
type CompleteMultipartUploadResult struct {
	XMLName  xml.Name `xml:"CompleteMultipartUploadResult"`
	Location string
	Bucket   string
	Key      string
	ETag     string
}

// ObjectList can used for sort the parts which needs in complete upload part
// sort.Sort(cos.ObjectList(opt.Parts))
type ObjectList []Object

func (o ObjectList) Len() int {
	return len(o)
}

func (o ObjectList) Swap(i, j int) {
	o[i], o[j] = o[j], o[i]
}

func (o ObjectList) Less(i, j int) bool { // rewrite the Less method from small to big
	return o[i].PartNumber < o[j].PartNumber
}

// CompleteMultipartUpload 用来实现完成整个分块上传。当您已经使用Upload Parts上传所有块以后，你可以用该API完成上传。
// 在使用该API时，您必须在Body中给出每一个块的PartNumber和ETag，用来校验块的准确性。
//
// 由于分块上传的合并需要数分钟时间，因而当合并分块开始的时候，COS就立即返回200的状态码，在合并的过程中，
// COS会周期性的返回空格信息来保持连接活跃，直到合并完成，COS会在Body中返回合并后块的内容。
//
// 当上传块小于1 MB的时候，在调用该请求时，会返回400 EntityTooSmall；
// 当上传块编号不连续的时候，在调用该请求时，会返回400 InvalidPart；
// 当请求Body中的块信息没有按序号从小到大排列的时候，在调用该请求时，会返回400 InvalidPartOrder；
// 当UploadId不存在的时候，在调用该请求时，会返回404 NoSuchUpload。
//
// 建议您及时完成分块上传或者舍弃分块上传，因为已上传但是未终止的块会占用存储空间进而产生存储费用。
//
// https://www.qcloud.com/document/product/436/7742
func (s *ObjectService) CompleteMultipartUpload(ctx context.Context, name, uploadID string, opt *CompleteMultipartUploadOptions) (*CompleteMultipartUploadResult, *Response, error) {
	u := fmt.Sprintf("/%s?uploadId=%s", encodeURIComponent(name), uploadID)
	var res CompleteMultipartUploadResult
	sendOpt := sendOptions{
		baseURL:   s.client.BaseURL.BucketURL,
		uri:       u,
		method:    http.MethodPost,
		optHeader: opt,
		body:      opt,
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

// AbortMultipartUpload 用来实现舍弃一个分块上传并删除已上传的块。当您调用Abort Multipart Upload时，
// 如果有正在使用这个Upload Parts上传块的请求，则Upload Parts会返回失败。当该UploadID不存在时，会返回404 NoSuchUpload。
//
// 建议您及时完成分块上传或者舍弃分块上传，因为已上传但是未终止的块会占用存储空间进而产生存储费用。
//
// https://www.qcloud.com/document/product/436/7740
func (s *ObjectService) AbortMultipartUpload(ctx context.Context, name, uploadID string) (*Response, error) {
	u := fmt.Sprintf("/%s?uploadId=%s", encodeURIComponent(name), uploadID)
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.BucketURL,
		uri:     u,
		method:  http.MethodDelete,
	}
	resp, err := s.client.doRetry(ctx, &sendOpt)
	return resp, err
}

// ObjectCopyPartOptions is the options of copy-part
type ObjectCopyPartOptions struct {
	XCosCopySource                  string `header:"x-cos-copy-source" url:"-"`
	XCosCopySourceRange             string `header:"x-cos-copy-source-range,omitempty" url:"-"`
	XCosCopySourceIfModifiedSince   string `header:"x-cos-copy-source-If-Modified-Since,omitempty" url:"-"`
	XCosCopySourceIfUnmodifiedSince string `header:"x-cos-copy-source-If-Unmodified-Since,omitempty" url:"-"`
	XCosCopySourceIfMatch           string `header:"x-cos-copy-source-If-Match,omitempty" url:"-"`
	XCosCopySourceIfNoneMatch       string `header:"x-cos-copy-source-If-None-Match,omitempty" url:"-"`
}

// CopyPartResult is the result CopyPart
type CopyPartResult struct {
	XMLName      xml.Name `xml:"CopyPartResult"`
	ETag         string
	LastModified string
}

// CopyPart 请求实现在初始化以后的分块上传，支持的块的数量为1到10000，块的大小为1 MB 到5 GB。
// 在每次请求Upload Part时候，需要携带partNumber和uploadID，partNumber为块的编号，支持乱序上传。
// ObjectCopyPartOptions的XCosCopySource为必填参数，格式为<bucket-name>-<app-id>.cos.<region-id>.myqcloud.com/<object-key>
// ObjectCopyPartOptions的XCosCopySourceRange指定源的Range，格式为bytes=<start>-<end>
//
// 当传入uploadID和partNumber都相同的时候，后传入的块将覆盖之前传入的块。当uploadID不存在时会返回404错误，NoSuchUpload.
//
// https://www.qcloud.com/document/product/436/7750
func (s *ObjectService) CopyPart(ctx context.Context, name, uploadID string, partNumber int, sourceURL string, opt *ObjectCopyPartOptions) (*CopyPartResult, *Response, error) {
	if opt == nil {
		opt = &ObjectCopyPartOptions{}
	}
	opt.XCosCopySource = sourceURL
	u := fmt.Sprintf("/%s?partNumber=%d&uploadId=%s", encodeURIComponent(name), partNumber, uploadID)
	var res CopyPartResult
	sendOpt := sendOptions{
		baseURL:   s.client.BaseURL.BucketURL,
		uri:       u,
		method:    http.MethodPut,
		optHeader: opt,
		result:    &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	// If the error occurs during the copy operation, the error response is embedded in the 200 OK response. This means that a 200 OK response can contain either a success or an error.
	if err == nil && resp != nil && resp.StatusCode == 200 {
		if res.ETag == "" {
			return &res, resp, errors.New("response 200 OK, but body contains an error")
		}
	}
	return &res, resp, err
}

type ObjectListUploadsOptions struct {
	Delimiter      string `url:"Delimiter,omitempty"`
	EncodingType   string `url:"EncodingType,omitempty"`
	Prefix         string `url:"Prefix"`
	MaxUploads     int    `url:"MaxUploads"`
	KeyMarker      string `url:"KeyMarker"`
	UploadIdMarker string `url:"UploadIDMarker"`
}

type ObjectListUploadsResult struct {
	XMLName            xml.Name                  `xml:"ListMultipartUploadsResult"`
	Bucket             string                    `xml:"Bucket,omitempty"`
	EncodingType       string                    `xml:"Encoding-Type,omitempty"`
	KeyMarker          string                    `xml:"KeyMarker,omitempty"`
	UploadIdMarker     string                    `xml:"UploadIdMarker,omitempty"`
	NextKeyMarker      string                    `xml:"NextKeyMarker,omitempty"`
	NextUploadIdMarker string                    `xml:"NextUploadIdMarker,omitempty"`
	MaxUploads         string                    `xml:"MaxUploads,omitempty"`
	IsTruncated        bool                      `xml:"IsTruncated,omitempty"`
	Prefix             string                    `xml:"Prefix,omitempty"`
	Delimiter          string                    `xml:"Delimiter,omitempty"`
	Upload             []ListUploadsResultUpload `xml:"Upload,omitempty"`
	CommonPrefixes     []string                  `xml:"CommonPrefixes>Prefix,omitempty"`
}

type ListUploadsResultUpload struct {
	Key          string     `xml:"Key,omitempty"`
	UploadID     string     `xml:"UploadId,omitempty"`
	StorageClass string     `xml:"StorageClass,omitempty"`
	Initiator    *Initiator `xml:"Initiator,omitempty"`
	Owner        *Owner     `xml:"Owner,omitempty"`
	Initiated    string     `xml:"Initiated,omitempty"`
}

func (s *ObjectService) ListUploads(ctx context.Context, opt *ObjectListUploadsOptions) (*ObjectListUploadsResult, *Response, error) {
	var res ObjectListUploadsResult
	sendOpt := &sendOptions{
		baseURL:  s.client.BaseURL.BucketURL,
		uri:      "/?uploads",
		method:   http.MethodGet,
		optQuery: opt,
		result:   &res,
	}
	resp, err := s.client.doRetry(ctx, sendOpt)
	return &res, resp, err
}

type MultiCopyOptions struct {
	OptCopy        *ObjectCopyOptions
	PartSize       int64
	ThreadPoolSize int
}

type CopyJobs struct {
	Name       string
	UploadId   string
	RetryTimes int
	Chunk      Chunk
	Opt        *ObjectCopyPartOptions
}

type CopyResults struct {
	PartNumber int
	Resp       *Response
	err        error
	res        *CopyPartResult
}

func copyworker(ctx context.Context, s *ObjectService, jobs <-chan *CopyJobs, results chan<- *CopyResults) {
	for j := range jobs {
		var copyres CopyResults
		j.Opt.XCosCopySourceRange = fmt.Sprintf("bytes=%d-%d", j.Chunk.OffSet, j.Chunk.OffSet+j.Chunk.Size-1)
		rt := j.RetryTimes
		for {
			res, resp, err := s.CopyPart(ctx, j.Name, j.UploadId, j.Chunk.Number, j.Opt.XCosCopySource, j.Opt)
			copyres.PartNumber = j.Chunk.Number
			copyres.Resp = resp
			copyres.err = err
			copyres.res = res
			if err != nil {
				rt--
				if rt == 0 {
					results <- &copyres
					break
				}
				time.Sleep(10 * time.Millisecond)
				continue
			}
			results <- &copyres
			break
		}
	}
}

func (s *ObjectService) innerHead(ctx context.Context, sourceURL string, opt *ObjectHeadOptions, id []string) (resp *Response, err error) {
	surl := strings.SplitN(sourceURL, "/", 2)
	if len(surl) < 2 {
		err = errors.New(fmt.Sprintf("sourceURL format error: %s", sourceURL))
		return
	}

	u, err := url.Parse(fmt.Sprintf("https://%s", surl[0]))
	if err != nil {
		return
	}
	b := &BaseURL{BucketURL: u}
	client := NewClient(b, &http.Client{
		Transport: s.client.client.Transport,
	})
	if len(id) > 0 {
		resp, err = client.Object.Head(ctx, surl[1], nil, id[0])
	} else {
		resp, err = client.Object.Head(ctx, surl[1], nil)
	}
	return
}

// 如果源对象大于5G，则采用分块复制的方式进行拷贝，此时源对象的元信息如果COPY
func (s *ObjectService) MultiCopy(ctx context.Context, name string, sourceURL string, opt *MultiCopyOptions, id ...string) (*ObjectCopyResult, *Response, error) {
	resp, err := s.innerHead(ctx, sourceURL, nil, id)
	if err != nil {
		return nil, nil, err
	}
	totalBytes := resp.ContentLength
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

	if opt == nil {
		opt = &MultiCopyOptions{}
	}
	chunks, partNum, err := SplitSizeIntoChunks(totalBytes, opt.PartSize*1024*1024)
	if err != nil {
		return nil, nil, err
	}
	if partNum == 0 || totalBytes < singleUploadMaxLength {
		if len(id) > 0 {
			return s.Copy(ctx, name, sourceURL, opt.OptCopy, id[0])
		} else {
			return s.Copy(ctx, name, sourceURL, opt.OptCopy)
		}
	}
	optini := CopyOptionsToMulti(opt.OptCopy)
	var uploadID string
	res, _, err := s.InitiateMultipartUpload(ctx, name, optini)
	if err != nil {
		return nil, nil, err
	}
	uploadID = res.UploadID

	var poolSize int
	if opt.ThreadPoolSize > 0 {
		poolSize = opt.ThreadPoolSize
	} else {
		poolSize = 1
	}

	chjobs := make(chan *CopyJobs, 100)
	chresults := make(chan *CopyResults, 10000)
	optcom := &CompleteMultipartUploadOptions{}

	for w := 1; w <= poolSize; w++ {
		go copyworker(ctx, s, chjobs, chresults)
	}

	go func() {
		for _, chunk := range chunks {
			partOpt := &ObjectCopyPartOptions{
				XCosCopySource: u,
			}
			job := &CopyJobs{
				Name:       name,
				RetryTimes: 3,
				UploadId:   uploadID,
				Chunk:      chunk,
				Opt:        partOpt,
			}
			chjobs <- job
		}
		close(chjobs)
	}()
	err = nil
	for i := 0; i < partNum; i++ {
		res := <-chresults
		if res.res == nil || res.err != nil {
			err = fmt.Errorf("UploadID %s, part %d failed to get resp content. error: %s", uploadID, res.PartNumber, res.err.Error())
			continue
		}
		etag := res.res.ETag
		optcom.Parts = append(optcom.Parts, Object{
			PartNumber: res.PartNumber, ETag: etag},
		)
	}
	close(chresults)
	if err != nil {
		return nil, nil, err
	}
	sort.Sort(ObjectList(optcom.Parts))

	v, resp, err := s.CompleteMultipartUpload(ctx, name, uploadID, optcom)
	if err != nil {
		s.AbortMultipartUpload(ctx, name, uploadID)
		return nil, resp, err
	}
	cpres := &ObjectCopyResult{
		ETag:      v.ETag,
		CRC64:     resp.Header.Get("x-cos-hash-crc64ecma"),
		VersionId: resp.Header.Get("x-cos-version-id"),
	}
	return cpres, resp, err
}

package cos

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"hash/crc64"
	"io"
	"net/http"
	"os"
	"strconv"
)

type CIService service

type PicOperations struct {
	IsPicInfo int                  `json:"is_pic_info,omitempty"`
	Rules     []PicOperationsRules `json:"rules,omitemtpy"`
}
type PicOperationsRules struct {
	Bucket string `json:"bucket,omitempty"`
	FileId string `json:"fileid"`
	Rule   string `json:"rule"`
}

func EncodePicOperations(pic *PicOperations) string {
	if pic == nil {
		return ""
	}
	bs, err := json.Marshal(pic)
	if err != nil {
		return ""
	}
	return string(bs)
}

type ImageProcessResult struct {
	XMLName        xml.Name          `xml:"UploadResult"`
	OriginalInfo   *PicOriginalInfo  `xml:"OriginalInfo,omitempty"`
	ProcessResults *PicProcessObject `xml:"ProcessResults>Object,omitempty"`
}
type PicOriginalInfo struct {
	Key       string        `xml:"Key,omitempty"`
	Location  string        `xml:"Location,omitempty"`
	ImageInfo *PicImageInfo `xml:"ImageInfo,omitempty"`
	ETag      string        `xml:"ETag,omitempty"`
}
type PicImageInfo struct {
	Format      string `xml:"Format,omitempty"`
	Width       int    `xml:"Width,omitempty"`
	Height      int    `xml:"Height,omitempty"`
	Quality     int    `xml:"Quality,omitempty"`
	Ave         string `xml:"Ave,omitempty"`
	Orientation int    `xml:"Orientation,omitempty"`
}
type PicProcessObject struct {
	Key             string       `xml:"Key,omitempty"`
	Location        string       `xml:"Location,omitempty"`
	Format          string       `xml:"Format,omitempty"`
	Width           int          `xml:"Width,omitempty"`
	Height          int          `xml:"Height,omitempty"`
	Size            int          `xml:"Size,omitempty"`
	Quality         int          `xml:"Quality,omitempty"`
	ETag            string       `xml:"ETag,omitempty"`
	WatermarkStatus int          `xml:"WatermarkStatus,omitempty"`
	CodeStatus      int          `xml:"CodeStatus,omitempty"`
	QRcodeInfo      []QRcodeInfo `xml:"QRcodeInfo,omitempty"`
}
type QRcodeInfo struct {
	CodeUrl      string        `xml:"CodeUrl,omitempty"`
	CodeLocation *CodeLocation `xml:"CodeLocation,omitempty"`
}
type CodeLocation struct {
	Point []string `xml:"Point,omitempty"`
}

type picOperationsHeader struct {
	PicOperations string `header:"Pic-Operations" xml:"-" url:"-"`
}

type ImageProcessOptions = PicOperations

// 云上数据处理 https://cloud.tencent.com/document/product/460/18147
func (s *CIService) ImageProcess(ctx context.Context, name string, opt *ImageProcessOptions) (*ImageProcessResult, *Response, error) {
	header := &picOperationsHeader{
		PicOperations: EncodePicOperations(opt),
	}
	var res ImageProcessResult
	sendOpt := sendOptions{
		baseURL:   s.client.BaseURL.BucketURL,
		uri:       "/" + encodeURIComponent(name) + "?image_process",
		method:    http.MethodPost,
		optHeader: header,
		result:    &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type ImageRecognitionOptions struct {
	CIProcess  string `url:"ci-process,omitempty"`
	DetectType string `url:"detect-type,omitempty"`
	DetectUrl  string `url:"detect-url,omitempty"`
	Interval   int    `url:"interval,omitempty"`
	MaxFrames  int    `url:"max-frames,omitempty"`
	BizType    string `url:"biz-type,omitempty"`
}

type ImageRecognitionResult struct {
	XMLName       xml.Name         `xml:"RecognitionResult"`
	PornInfo      *RecognitionInfo `xml:"PornInfo,omitempty"`
	TerroristInfo *RecognitionInfo `xml:"TerroristInfo,omitempty"`
	PoliticsInfo  *RecognitionInfo `xml:"PoliticsInfo,omitempty"`
	AdsInfo       *RecognitionInfo `xml:"AdsInfo,omitempty"`
}
type RecognitionInfo struct {
	Code          int            `xml:"Code,omitempty"`
	Msg           string         `xml:"Msg,omitempty"`
	HitFlag       int            `xml:"HitFlag,omitempty"`
	Score         int            `xml:"Score,omitempty"`
	Label         string         `xml:"Label,omitempty"`
	Count         int            `xml:"Count,omitempty"`
	SubLabel      string         `xml:"SubLabel,omitempty"`
	Keywords      string         `xml:"Keywords,omitempty"`
	OcrResults    []OcrResult    `xml:"OcrResults,omitempty"`
	ObjectResults []ObjectResult `xml:"ObjectResults,omitempty"`
	LibResults    []LibResult    `xml:"LibResults,omitempty"`
}

// 图片审核 https://cloud.tencent.com/document/product/460/37318
func (s *CIService) ImageRecognition(ctx context.Context, name string, DetectType string) (*ImageRecognitionResult, *Response, error) {
	opt := &ImageRecognitionOptions{
		CIProcess:  "sensitive-content-recognition",
		DetectType: DetectType,
	}
	var res ImageRecognitionResult
	sendOpt := sendOptions{
		baseURL:  s.client.BaseURL.BucketURL,
		uri:      "/" + encodeURIComponent(name),
		method:   http.MethodGet,
		optQuery: opt,
		result:   &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}
// 图片审核 支持detect-url等全部参数
func (s *CIService) ImageAuditing(ctx context.Context, name string, opt *ImageRecognitionOptions) (*ImageRecognitionResult, *Response, error) {
	var res ImageRecognitionResult
	sendOpt := sendOptions{
		baseURL:  s.client.BaseURL.BucketURL,
		uri:      "/" + encodeURIComponent(name),
		method:   http.MethodGet,
		optQuery: opt,
		result:   &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type PutVideoAuditingJobOptions struct {
	XMLName     xml.Name              `xml:"Request"`
	InputObject string                `xml:"Input>Object"`
	Conf        *VideoAuditingJobConf `xml:"Conf"`
}
type VideoAuditingJobConf struct {
	DetectType    string                       `xml:",omitempty"`
	Snapshot      *PutVideoAuditingJobSnapshot `xml:",omitempty"`
	Callback      string                       `xml:",omitempty"`
	BizType       string                       `xml:",omitempty"`
	DetectContent int                          `xml:",omitempty"`
}
type PutVideoAuditingJobSnapshot struct {
	Mode         string  `xml:",omitempty"`
	Count        int     `xml:",omitempty"`
	TimeInterval float32 `xml:",omitempty"`
	Start        float32 `xml:",omitempty"`
}

type PutVideoAuditingJobResult struct {
	XMLName    xml.Name `xml:"Response"`
	JobsDetail struct {
		JobId        string `xml:"JobId,omitempty"`
		State        string `xml:"State,omitempty"`
		CreationTime string `xml:"CreationTime,omitempty"`
		Object       string `xml:"Object,omitempty"`
	} `xml:"JobsDetail,omitempty"`
}

// 视频审核-创建任务 https://cloud.tencent.com/document/product/460/46427
func (s *CIService) PutVideoAuditingJob(ctx context.Context, opt *PutVideoAuditingJobOptions) (*PutVideoAuditingJobResult, *Response, error) {
	var res PutVideoAuditingJobResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/video/auditing",
		method:  http.MethodPost,
		body:    opt,
		result:  &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type GetVideoAuditingJobResult struct {
	XMLName        xml.Name           `xml:"Response"`
	JobsDetail     *AuditingJobDetail `xml:",omitempty"`
	NonExistJobIds string             `xml:",omitempty"`
}
type AuditingJobDetail struct {
	Code          string                       `xml:",omitempty"`
	Message       string                       `xml:",omitempty"`
	JobId         string                       `xml:",omitempty"`
	State         string                       `xml:",omitempty"`
	CreationTime  string                       `xml:",omitempty"`
	Object        string                       `xml:",omitempty"`
	SnapshotCount string                       `xml:",omitempty"`
	Result        int                          `xml:",omitempty"`
	PornInfo      *RecognitionInfo             `xml:",omitempty"`
	TerrorismInfo *RecognitionInfo             `xml:",omitempty"`
	PoliticsInfo  *RecognitionInfo             `xml:",omitempty"`
	AdsInfo       *RecognitionInfo             `xml:",omitempty"`
	Snapshot      *GetVideoAuditingJobSnapshot `xml:",omitempty"`
	AudioSection  *AudioSectionResult          `xml:",omitempty"`
}
type GetVideoAuditingJobSnapshot struct {
	Url           string           `xml:",omitempty"`
	SnapshotTime  string           `xml:",omitempty"`
	Text          string           `xml:",omitempty"`
	PornInfo      *RecognitionInfo `xml:",omitempty"`
	TerrorismInfo *RecognitionInfo `xml:",omitempty"`
	PoliticsInfo  *RecognitionInfo `xml:",omitempty"`
	AdsInfo       *RecognitionInfo `xml:",omitempty"`
}
type AudioSectionResult struct {
	Url           string           `xml:",omitempty"`
	Text          string           `xml:",omitempty"`
	OffsetTime    int              `xml:",omitempty"`
	Duration      int              `xml:",omitempty"`
	PornInfo      *RecognitionInfo `xml:",omitempty"`
	TerrorismInfo *RecognitionInfo `xml:",omitempty"`
	PoliticsInfo  *RecognitionInfo `xml:",omitempty"`
	AdsInfo       *RecognitionInfo `xml:",omitempty"`
}

// 视频审核-查询任务 https://cloud.tencent.com/document/product/460/46926
func (s *CIService) GetVideoAuditingJob(ctx context.Context, jobid string) (*GetVideoAuditingJobResult, *Response, error) {
	var res GetVideoAuditingJobResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/video/auditing/" + jobid,
		method:  http.MethodGet,
		result:  &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type PutAudioAuditingJobOptions struct {
	XMLName     xml.Name              `xml:"Request"`
	InputObject string                `xml:"Input>Object,omitempty"`
	InputUrl    string                `xml:"Input>Url,omitempty"`
	Conf        *AudioAuditingJobConf `xml:"Conf"`
}
type AudioAuditingJobConf struct {
	DetectType      string `xml:",omitempty"`
	Callback        string `xml:",omitempty"`
	CallbackVersion string `xml:",omitempty"`
	BizType         string `xml:",omitempty"`
}
type PutAudioAuditingJobResult PutVideoAuditingJobResult

// 音频审核-创建任务 https://cloud.tencent.com/document/product/460/53395
func (s *CIService) PutAudioAuditingJob(ctx context.Context, opt *PutAudioAuditingJobOptions) (*PutAudioAuditingJobResult, *Response, error) {
	var res PutAudioAuditingJobResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/audio/auditing",
		method:  http.MethodPost,
		body:    opt,
		result:  &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type GetAudioAuditingJobResult struct {
	XMLName        xml.Name                `xml:"Response"`
	JobsDetail     *AudioAuditingJobDetail `xml:",omitempty"`
	NonExistJobIds string                  `xml:",omitempty"`
}
type AudioAuditingJobDetail struct {
	Code          string              `xml:",omitempty"`
	Message       string              `xml:",omitempty"`
	JobId         string              `xml:",omitempty"`
	State         string              `xml:",omitempty"`
	CreationTime  string              `xml:",omitempty"`
	Object        string              `xml:",omitempty"`
	Url           string              `xml:",omitempty"`
	Result        int                 `xml:",omitempty"`
	AudioText     string              `xml:",omitempty"`
	PornInfo      *RecognitionInfo    `xml:",omitempty"`
	TerrorismInfo *RecognitionInfo    `xml:",omitempty"`
	PoliticsInfo  *RecognitionInfo    `xml:",omitempty"`
	AdsInfo       *RecognitionInfo    `xml:",omitempty"`
	Section       *AudioSectionResult `xml:",omitempty"`
}

// 音频审核-查询任务 https://cloud.tencent.com/document/product/460/53396
func (s *CIService) GetAudioAuditingJob(ctx context.Context, jobid string) (*GetAudioAuditingJobResult, *Response, error) {
	var res GetAudioAuditingJobResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/audio/auditing/" + jobid,
		method:  http.MethodGet,
		result:  &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type PutTextAuditingJobOptions struct {
	XMLName      xml.Name             `xml:"Request"`
	InputObject  string               `xml:"Input>Object,omitempty"`
	InputContent string               `xml:"Input>Content,omitempty"`
	Conf         *TextAuditingJobConf `xml:"Conf"`
}
type TextAuditingJobConf struct {
	DetectType string `xml:",omitempty"`
	Callback   string `xml:",omitempty"`
	BizType    string `xml:",omitempty"`
}
type PutTextAuditingJobResult GetTextAuditingJobResult

// 文本审核-创建任务 https://cloud.tencent.com/document/product/436/56289
func (s *CIService) PutTextAuditingJob(ctx context.Context, opt *PutTextAuditingJobOptions) (*PutTextAuditingJobResult, *Response, error) {
	var res PutTextAuditingJobResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/text/auditing",
		method:  http.MethodPost,
		body:    opt,
		result:  &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type GetTextAuditingJobResult struct {
	XMLName        xml.Name               `xml:"Response"`
	JobsDetail     *TextAuditingJobDetail `xml:",omitempty"`
	NonExistJobIds string                 `xml:",omitempty"`
}
type TextAuditingJobDetail struct {
	Code          string             `xml:",omitempty"`
	Message       string             `xml:",omitempty"`
	JobId         string             `xml:",omitempty"`
	State         string             `xml:",omitempty"`
	CreationTime  string             `xml:",omitempty"`
	Object        string             `xml:",omitempty"`
	SectionCount  int                `xml:",omitempty"`
	Result        int                `xml:",omitempty"`
	PornInfo      *RecognitionInfo   `xml:",omitempty"`
	TerrorismInfo *RecognitionInfo   `xml:",omitempty"`
	PoliticsInfo  *RecognitionInfo   `xml:",omitempty"`
	AdsInfo       *RecognitionInfo   `xml:",omitempty"`
	IllegalInfo   *RecognitionInfo   `xml:",omitempty"`
	AbuseInfo     *RecognitionInfo   `xml:",omitempty"`
	Section       *TextSectionResult `xml:",omitempty"`
}
type TextSectionResult struct {
	StartByte     int              `xml:",omitempty"`
	PornInfo      *RecognitionInfo `xml:",omitempty"`
	TerrorismInfo *RecognitionInfo `xml:",omitempty"`
	PoliticsInfo  *RecognitionInfo `xml:",omitempty"`
	AdsInfo       *RecognitionInfo `xml:",omitempty"`
	IllegalInfo   *RecognitionInfo `xml:",omitempty"`
	AbuseInfo     *RecognitionInfo `xml:",omitempty"`
}

// 文本审核-查询任务 https://cloud.tencent.com/document/product/436/56288
func (s *CIService) GetTextAuditingJob(ctx context.Context, jobid string) (*GetTextAuditingJobResult, *Response, error) {
	var res GetTextAuditingJobResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/text/auditing/" + jobid,
		method:  http.MethodGet,
		result:  &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type PutDocumentAuditingJobOptions struct {
	XMLName   xml.Name                 `xml:"Request"`
	InputUrl  string                   `xml:"Input>Url,omitempty"`
	InputType string                   `xml:"Input>Type,omitempty"`
	Conf      *DocumentAuditingJobConf `xml:"Conf"`
}
type DocumentAuditingJobConf TextAuditingJobConf
type PutDocumentAuditingJobResult PutVideoAuditingJobResult

// 文档审核-创建任务 https://cloud.tencent.com/document/product/436/59381
func (s *CIService) PutDocumentAuditingJob(ctx context.Context, opt *PutDocumentAuditingJobOptions) (*PutDocumentAuditingJobResult, *Response, error) {
	var res PutDocumentAuditingJobResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/document/auditing",
		method:  http.MethodPost,
		body:    opt,
		result:  &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type GetDocumentAuditingJobResult struct {
	XMLName        xml.Name                   `xml:"Response"`
	JobsDetail     *DocumentAuditingJobDetail `xml:",omitempty"`
	NonExistJobIds string                     `xml:",omitempty"`
}
type DocumentAuditingJobDetail struct {
	Code         string                   `xml:",omitempty"`
	Message      string                   `xml:",omitempty"`
	JobId        string                   `xml:",omitempty"`
	State        string                   `xml:",omitempty"`
	CreationTime string                   `xml:",omitempty"`
	Suggestion   int                      `xml:",omitempty"`
	Url          string                   `xml:",omitempty"`
	PageCount    int                      `xml:",omitempty"`
	Labels       *DocumentResultInfo      `xml:",omitempty"`
	PageSegment  *DocumentPageSegmentInfo `xml:",omitempty"`
}
type DocumentResultInfo struct {
	PornInfo      *RecognitionInfo `xml:",omitempty"`
	TerrorismInfo *RecognitionInfo `xml:",omitempty"`
	PoliticsInfo  *RecognitionInfo `xml:",omitempty"`
	AdsInfo       *RecognitionInfo `xml:",omitempty"`
}
type DocumentPageSegmentInfo struct {
	Results []DocumentPageSegmentResultResult `xml:",omitempty"`
}
type DocumentPageSegmentResultResult struct {
	Url           string           `xml:",omitempty"`
	Text          string           `xml:",omitempty"`
	PageNumber    int              `xml:",omitempty"`
	SheetNumber   int              `xml:",omitempty"`
	PornInfo      *RecognitionInfo `xml:",omitempty"`
	TerrorismInfo *RecognitionInfo `xml:",omitempty"`
	PoliticsInfo  *RecognitionInfo `xml:",omitempty"`
	AdsInfo       *RecognitionInfo `xml:",omitempty"`
}
type OcrResult struct {
	Text     string    `xml:"Text"`
	Keywords []string  `xml:"Keywords"`
	Location *Location `xml:"Location,omitempty"`
}
type ObjectResult struct {
	Name     string    `xml:"Name"`
	Location *Location `xml:"Location,omitempty"`
}
type LibResult struct {
	ImageId string `xml:"ImageId"`
	Score   uint32 `xml:"Score"`
}
type Location struct {
	X      float64 `json:"X"`      // 左上角横坐标
	Y      float64 `json:"Y"`      // 左上角纵坐标
	Width  float64 `json:"Width"`  // 宽度
	Height float64 `json:"Height"` // 高度
	Rotate float64 `json:"Rotate"` // 检测框的旋转角度
}

// 文档审核-查询任务 https://cloud.tencent.com/document/product/436/59382
func (s *CIService) GetDocumentAuditingJob(ctx context.Context, jobid string) (*GetDocumentAuditingJobResult, *Response, error) {
	var res GetDocumentAuditingJobResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/document/auditing/" + jobid,
		method:  http.MethodGet,
		result:  &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

// 图片持久化处理-上传时处理 https://cloud.tencent.com/document/product/460/18147
// 盲水印-上传时添加 https://cloud.tencent.com/document/product/460/19017
// 二维码识别-上传时识别 https://cloud.tencent.com/document/product/460/37513
func (s *CIService) Put(ctx context.Context, name string, r io.Reader, uopt *ObjectPutOptions) (*ImageProcessResult, *Response, error) {
	if r == nil {
		return nil, nil, fmt.Errorf("reader is nil")
	}
	if err := CheckReaderLen(r); err != nil {
		return nil, nil, err
	}
	opt := CloneObjectPutOptions(uopt)
	totalBytes, err := GetReaderLen(r)
	if err != nil && opt != nil && opt.Listener != nil {
		if opt.ContentLength == 0 {
			return nil, nil, err
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

	var res ImageProcessResult
	sendOpt := sendOptions{
		baseURL:   s.client.BaseURL.BucketURL,
		uri:       "/" + encodeURIComponent(name),
		method:    http.MethodPut,
		body:      reader,
		optHeader: opt,
		result:    &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)

	return &res, resp, err
}

// ci put object from local file
func (s *CIService) PutFromFile(ctx context.Context, name string, filePath string, opt *ObjectPutOptions) (*ImageProcessResult, *Response, error) {
	fd, err := os.Open(filePath)
	if err != nil {
		return nil, nil, err
	}
	defer fd.Close()

	return s.Put(ctx, name, fd, opt)
}

// 基本图片处理 https://cloud.tencent.com/document/product/460/36540
// 盲水印-下载时添加 https://cloud.tencent.com/document/product/460/19017
func (s *CIService) Get(ctx context.Context, name string, operation string, opt *ObjectGetOptions, id ...string) (*Response, error) {
	var u string
	if len(id) == 1 {
		u = fmt.Sprintf("/%s?versionId=%s&%s", encodeURIComponent(name), id[0], operation)
	} else if len(id) == 0 {
		u = fmt.Sprintf("/%s?%s", encodeURIComponent(name), operation)
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
	resp, err := s.client.send(ctx, &sendOpt)

	if opt != nil && opt.Listener != nil {
		if err == nil && resp != nil {
			if totalBytes, e := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64); e == nil {
				resp.Body = TeeReader(resp.Body, nil, totalBytes, opt.Listener)
			}
		}
	}
	return resp, err
}

func (s *CIService) GetToFile(ctx context.Context, name, localpath, operation string, opt *ObjectGetOptions, id ...string) (*Response, error) {
	resp, err := s.Get(ctx, name, operation, opt, id...)
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

type GetQRcodeResult struct {
	XMLName     xml.Name    `xml:"Response"`
	CodeStatus  int         `xml:"CodeStatus,omitempty"`
	QRcodeInfo  *QRcodeInfo `xml:"QRcodeInfo,omitempty"`
	ResultImage string      `xml:"ResultImage,omitempty"`
}

// 二维码识别-下载时识别 https://cloud.tencent.com/document/product/436/54070
func (s *CIService) GetQRcode(ctx context.Context, name string, cover int, opt *ObjectGetOptions, id ...string) (*GetQRcodeResult, *Response, error) {
	var u string
	if len(id) == 1 {
		u = fmt.Sprintf("/%s?versionId=%s&ci-process=QRcode&cover=%v", encodeURIComponent(name), id[0], cover)
	} else if len(id) == 0 {
		u = fmt.Sprintf("/%s?ci-process=QRcode&cover=%v", encodeURIComponent(name), cover)
	} else {
		return nil, nil, errors.New("wrong params")
	}

	var res GetQRcodeResult
	sendOpt := sendOptions{
		baseURL:   s.client.BaseURL.BucketURL,
		uri:       u,
		method:    http.MethodGet,
		optQuery:  opt,
		optHeader: opt,
		result:    &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type GenerateQRcodeOptions struct {
	QRcodeContent string `url:"qrcode-content,omitempty"`
	Mode          int    `url:"mode,omitempty"`
	Width         int    `url:"width,omitempty"`
}
type GenerateQRcodeResult struct {
	XMLName     xml.Name `xml:"Response"`
	ResultImage string   `xml:"ResultImage,omitempty"`
}

// 二维码生成 https://cloud.tencent.com/document/product/436/54071
func (s *CIService) GenerateQRcode(ctx context.Context, opt *GenerateQRcodeOptions) (*GenerateQRcodeResult, *Response, error) {
	var res GenerateQRcodeResult
	sendOpt := &sendOptions{
		baseURL:  s.client.BaseURL.BucketURL,
		uri:      "/?ci-process=qrcode-generate",
		method:   http.MethodGet,
		optQuery: opt,
		result:   &res,
	}
	resp, err := s.client.send(ctx, sendOpt)
	return &res, resp, err
}

func (s *CIService) GenerateQRcodeToFile(ctx context.Context, filePath string, opt *GenerateQRcodeOptions) (*GenerateQRcodeResult, *Response, error) {
	res, resp, err := s.GenerateQRcode(ctx, opt)
	if err != nil {
		return res, resp, err
	}

	// If file exist, overwrite it
	fd, err := os.OpenFile(filePath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0660)
	if err != nil {
		return res, resp, err
	}
	defer fd.Close()

	bs, err := base64.StdEncoding.DecodeString(res.ResultImage)
	if err != nil {
		return res, resp, err
	}
	fb := bytes.NewReader(bs)
	_, err = io.Copy(fd, fb)

	return res, resp, err
}

// 开通 Guetzli 压缩 https://cloud.tencent.com/document/product/460/30112
func (s *CIService) PutGuetzli(ctx context.Context) (*Response, error) {
	sendOpt := &sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/?guetzli",
		method:  http.MethodPut,
	}
	resp, err := s.client.send(ctx, sendOpt)
	return resp, err
}

type GetGuetzliResult struct {
	XMLName       xml.Name `xml:"GuetzliStatus"`
	GuetzliStatus string   `xml:",chardata"`
}

// 查询 Guetzli 状态 https://cloud.tencent.com/document/product/460/30111
func (s *CIService) GetGuetzli(ctx context.Context) (*GetGuetzliResult, *Response, error) {
	var res GetGuetzliResult
	sendOpt := &sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/?guetzli",
		method:  http.MethodGet,
		result:  &res,
	}
	resp, err := s.client.send(ctx, sendOpt)
	return &res, resp, err
}

// 关闭 Guetzli 压缩 https://cloud.tencent.com/document/product/460/30113
func (s *CIService) DeleteGuetzli(ctx context.Context) (*Response, error) {
	sendOpt := &sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/?guetzli",
		method:  http.MethodDelete,
	}
	resp, err := s.client.send(ctx, sendOpt)
	return resp, err
}

package cos

import (
	"context"
	"encoding/xml"
	"net/http"
)

type DocProcessJobInput struct {
	Object string `xml:"Object,omitempty"`
}

type DocProcessJobOutput struct {
	Region string `xml:"Region,omitempty"`
	Bucket string `xml:"Bucket,omitempty"`
	Object string `xml:"Object,omitempty"`
}

type DocProcessJobDocProcess struct {
	SrcType        string `xml:"SrcType,omitempty"`
	TgtType        string `xml:"TgtType,omitempty"`
	SheetId        int    `xml:"SheetId,omitempty"`
	StartPage      int    `xml:"StartPage,omitempty"`
	EndPage        int    `xml:"EndPage,omitempty"`
	ImageParams    string `xml:"ImageParams,omitempty"`
	DocPassword    string `xml:"DocPassword,omitempty"`
	Comments       int    `xml:"Comments,omitempty"`
	PaperDirection int    `xml:"PaperDirection,omitempty"`
	Quality        int    `xml:"Quality,omitempty"`
	Zoom           int    `xml:"Zoom,omitempty"`
}

type DocProcessJobDocProcessResult struct {
	FailPageCount  int    `xml:",omitempty"`
	SuccPageCount  int    `xml:"SuccPageCount,omitempty"`
	TaskId         string `xml:"TaskId,omitempty"`
	TgtType        string `xml:"TgtType,omitempty"`
	TotalPageCount int    `xml:"TotalPageCount,omitempty"`
	PageInfo       struct {
		PageNo int    `xml:"PageNo,omitempty"`
		TgtUri string `xml:"TgtUri,omitempty"`
	} `xml:"PageInfo,omitempty"`
}

type DocProcessJobOperation struct {
	Output           *DocProcessJobOutput           `xml:"Output,omitempty"`
	DocProcess       *DocProcessJobDocProcess       `xml:"DocProcess,omitempty"`
	DocProcessResult *DocProcessJobDocProcessResult `xml:"DocProcessResult,omitempty"`
}

type DocProcessJobDetail struct {
	Code         string                  `xml:"Code,omitempty"`
	Message      string                  `xml:"Message,omitempty"`
	JobId        string                  `xml:"JobId,omitempty"`
	Tag          string                  `xml:"Tag,omitempty"`
	State        string                  `xml:"State,omitempty"`
	CreationTime string                  `xml:"CreationTime,omitempty"`
	QueueId      string                  `xml:"QueueId,omitempty"`
	Input        *DocProcessJobInput     `xml:"Input,omitempty"`
	Operation    *DocProcessJobOperation `xml:"Operation,omitempty"`
}

type CreateDocProcessJobsOptions struct {
	XMLName   xml.Name                `xml:"Request"`
	Tag       string                  `xml:"Tag,omitempty"`
	Input     *DocProcessJobInput     `xml:"Input,omitempty"`
	Operation *DocProcessJobOperation `xml:"Operation,omitempty"`
	QueueId   string                  `xml:"QueueId,omitempty"`
}

type CreateDocProcessJobsResult struct {
	XMLName    xml.Name            `xml:"Response"`
	JobsDetail DocProcessJobDetail `xml:"JobsDetail,omitempty"`
}

// 创建文档预览任务 https://cloud.tencent.com/document/product/436/54056
func (s *CIService) CreateDocProcessJobs(ctx context.Context, opt *CreateDocProcessJobsOptions) (*CreateDocProcessJobsResult, *Response, error) {
	var res CreateDocProcessJobsResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/doc_jobs",
		method:  http.MethodPost,
		body:    opt,
		result:  &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type DescribeDocProcessJobResult struct {
	XMLName        xml.Name             `xml:"Response"`
	JobsDetail     *DocProcessJobDetail `xml:"JobsDetail,omitempty"`
	NonExistJobIds string               `xml:"NonExistJobIds,omitempty"`
}

// 查询文档预览任务 https://cloud.tencent.com/document/product/436/54095
func (s *CIService) DescribeDocProcessJob(ctx context.Context, jobid string) (*DescribeDocProcessJobResult, *Response, error) {
	var res DescribeDocProcessJobResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/doc_jobs/" + jobid,
		method:  http.MethodGet,
		result:  &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type DescribeDocProcessJobsOptions struct {
	QueueId           string `url:"queueId,omitempty"`
	Tag               string `url:"tag,omitempty"`
	OrderByTime       string `url:"orderByTime,omitempty"`
	NextToken         string `url:"nextToken,omitempty"`
	Size              int    `url:"size,omitempty"`
	States            string `url:"states,omitempty"`
	StartCreationTime string `url:"startCreationTime,omitempty"`
	EndCreationTime   string `url:"endCreationTime,omitempty"`
}

type DescribeDocProcessJobsResult struct {
	XMLName    xml.Name              `xml:"Response"`
	JobsDetail []DocProcessJobDetail `xml:"JobsDetail,omitempty"`
	NextToken  string                `xml:"NextToken,omitempty"`
}

// 拉取符合条件的文档预览任务 https://cloud.tencent.com/document/product/436/54096
func (s *CIService) DescribeDocProcessJobs(ctx context.Context, opt *DescribeDocProcessJobsOptions) (*DescribeDocProcessJobsResult, *Response, error) {
	var res DescribeDocProcessJobsResult
	sendOpt := sendOptions{
		baseURL:  s.client.BaseURL.CIURL,
		uri:      "/doc_jobs",
		optQuery: opt,
		method:   http.MethodGet,
		result:   &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type DescribeDocProcessQueuesOptions struct {
	QueueIds   string `url:"queueIds,omitempty"`
	State      string `url:"state,omitempty"`
	PageNumber int    `url:"pageNumber,omitempty"`
	PageSize   int    `url:"pageSize,omitempty"`
}

type DescribeDocProcessQueuesResult struct {
	XMLName      xml.Name          `xml:"Response"`
	RequestId    string            `xml:"RequestId,omitempty"`
	TotalCount   int               `xml:"TotalCount,omitempty"`
	PageNumber   int               `xml:"PageNumber,omitempty"`
	PageSize     int               `xml:"PageSize,omitempty"`
	QueueList    []DocProcessQueue `xml:"QueueList,omitempty"`
	NonExistPIDs []string          `xml:"NonExistPIDs,omitempty"`
}

type DocProcessQueue struct {
	QueueId       string                       `xml:"QueueId,omitempty"`
	Name          string                       `xml:"Name,omitempty"`
	State         string                       `xml:"State,omitempty"`
	MaxSize       int                          `xml:"MaxSize,omitempty"`
	MaxConcurrent int                          `xml:"MaxConcurrent,omitempty"`
	UpdateTime    string                       `xml:"UpdateTime,omitempty"`
	CreateTime    string                       `xml:"CreateTime,omitempty"`
	NotifyConfig  *DocProcessQueueNotifyConfig `xml:"NotifyConfig,omitempty"`
}

type DocProcessQueueNotifyConfig struct {
	Url   string `xml:"Url,omitempty"`
	State string `xml:"State,omitempty"`
	Type  string `xml:"Type,omitempty"`
	Event string `xml:"Event,omitempty"`
}

// 查询文档预览队列 https://cloud.tencent.com/document/product/436/54055
func (s *CIService) DescribeDocProcessQueues(ctx context.Context, opt *DescribeDocProcessQueuesOptions) (*DescribeDocProcessQueuesResult, *Response, error) {
	var res DescribeDocProcessQueuesResult
	sendOpt := sendOptions{
		baseURL:  s.client.BaseURL.CIURL,
		uri:      "/docqueue",
		optQuery: opt,
		method:   http.MethodGet,
		result:   &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type UpdateDocProcessQueueOptions struct {
	XMLName      xml.Name                     `xml:"Request"`
	Name         string                       `xml:"Name,omitempty"`
	QueueID      string                       `xml:"QueueID,omitempty"`
	State        string                       `xml:"State,omitempty"`
	NotifyConfig *DocProcessQueueNotifyConfig `xml:"NotifyConfig,omitempty"`
}

type UpdateDocProcessQueueResult struct {
	XMLName   xml.Name         `xml:"Response"`
	RequestId string           `xml:"RequestId"`
	Queue     *DocProcessQueue `xml:"Queue"`
}

// 更新文档预览队列 https://cloud.tencent.com/document/product/436/54094
func (s *CIService) UpdateDocProcessQueue(ctx context.Context, opt *UpdateDocProcessQueueOptions) (*UpdateDocProcessQueueResult, *Response, error) {
	var res UpdateDocProcessQueueResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/docqueue/" + opt.QueueID,
		body:    opt,
		method:  http.MethodPut,
		result:  &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type DescribeDocProcessBucketsOptions struct {
	Regions     string `url:"regions,omitempty"`
	BucketNames string `url:"bucketNames,omitempty"`
	BucketName  string `url:"bucketName,omitempty"`
	PageNumber  int    `url:"pageNumber,omitempty"`
	PageSize    int    `url:"pageSize,omitempty"`
}

type DescribeDocProcessBucketsResult struct {
	XMLName       xml.Name           `xml:"Response"`
	RequestId     string             `xml:"RequestId,omitempty"`
	TotalCount    int                `xml:"TotalCount,omitempty"`
	PageNumber    int                `xml:"PageNumber,omitempty"`
	PageSize      int                `xml:"PageSize,omitempty"`
	DocBucketList []DocProcessBucket `xml:"DocBucketList,omitempty"`
}
type DocProcessBucket struct {
	BucketId      string `xml:"BucketId,omitempty"`
	Name          string `xml:"Name,omitempty"`
	Region        string `xml:"Region,omitempty"`
	CreateTime    string `xml:"CreateTime,omitempty"`
	AliasBucketId string `xml:"AliasBucketId,omitempty"`
}

// 查询文档预览开通状态 https://cloud.tencent.com/document/product/436/54057
func (s *CIService) DescribeDocProcessBuckets(ctx context.Context, opt *DescribeDocProcessBucketsOptions) (*DescribeDocProcessBucketsResult, *Response, error) {
	var res DescribeDocProcessBucketsResult
	sendOpt := sendOptions{
		baseURL:  s.client.BaseURL.CIURL,
		uri:      "/docbucket",
		optQuery: opt,
		method:   http.MethodGet,
		result:   &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type DocPreviewOptions struct {
	SrcType             string `url:"srcType,omitempty"`
	Page                int    `url:"page,omitempty"`
	ImageParams         string `url:"ImageParams,omitempty"`
	Sheet               int    `url:"sheet,omitempty"`
	DstType             string `url:"dstType,omitempty"`
	Password            string `url:"password,omitempty"`
	Comment             int    `url:"comment,omitempty"`
	ExcelPaperDirection int    `url:"excelPaperDirection,omitempty"`
	Quality             int    `url:"quality,omitempty"`
	Zoom                int    `url:"zoom,omitempty"`
}

// 同步请求接口 https://cloud.tencent.com/document/product/436/54058
func (s *CIService) DocPreview(ctx context.Context, name string, opt *DocPreviewOptions) (*Response, error) {
	sendOpt := sendOptions{
		baseURL:          s.client.BaseURL.BucketURL,
		uri:              "/" + encodeURIComponent(name) + "?ci-process=doc-preview",
		optQuery:         opt,
		method:           http.MethodGet,
		disableCloseBody: true,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return resp, err
}

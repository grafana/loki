package cos

import (
	"context"
	"encoding/xml"
	"net/http"
)

type JobInput struct {
	Object string `xml:"Object,omitempty"`
}

type JobOutput struct {
	Region string `xml:"Region,omitempty"`
	Bucket string `xml:"Bucket,omitempty"`
	Object string `xml:"Object,omitempty"`
}

type Container struct {
	Format string `xml:"Format"`
}

type Video struct {
	Codec         string `xml:"Codec"`
	Width         string `xml:"Width"`
	Height        string `xml:"Height"`
	Fps           string `xml:"Fps"`
	Remove        string `xml:"Remove,omitempty"`
	Profile       string `xml:"Profile"`
	Bitrate       string `xml:"Bitrate"`
	Crf           string `xml:"Crf"`
	Gop           string `xml:"Gop"`
	Preset        string `xml:"Preset"`
	Bufsize       string `xml:"Bufsize"`
	Maxrate       string `xml:"Maxrate"`
	HlsTsTime     string `xml:"HlsTsTime"`
	Pixfmt        string `xml:"Pixfmt"`
	LongShortMode string `xml:"LongShortMode"`
}

type TimeInterval struct {
	Start    string `xml:"Start"`
	Duration string `xml:"Duration"`
}

type Audio struct {
	Codec      string `xml:"Codec"`
	Samplerate string `xml:"Samplerate"`
	Bitrate    string `xml:"Bitrate"`
	Channels   string `xml:"Channels"`
	Remove     string `xml:"Remove,omitempty"`
}

type TransConfig struct {
	AdjDarMethod          string `xml:"AdjDarMethod"`
	IsCheckReso           string `xml:"IsCheckReso"`
	ResoAdjMethod         string `xml:"ResoAdjMethod"`
	IsCheckVideoBitrate   string `xml:"IsCheckVideoBitrate"`
	VideoBitrateAdjMethod string `xml:"VideoBitrateAdjMethod"`
	IsCheckAudioBitrate   string `xml:"IsCheckAudioBitrate"`
	AudioBitrateAdjMethod string `xml:"AudioBitrateAdjMethod"`
}

type Transcode struct {
	Container    *Container    `xml:"Container,omitempty"`
	Video        *Video        `xml:"Video,omitempty"`
	TimeInterval *TimeInterval `xml:"TimeInterval,omitempty"`
	Audio        *Audio        `xml:"Audio,omitempty"`
	TransConfig  *TransConfig  `xml:"TransConfig,omitempty"`
}

type Image struct {
	Url          string `xml:"Url,omitempty"`
	Mode         string `xml:"Mode,omitempty"`
	Width        string `xml:"Width,omitempty"`
	Height       string `xml:"Height,omitempty"`
	Transparency string `xml:"Transparency,omitempty"`
	Background   string `xml:"Background,omitempty"`
}

type Text struct {
	FontSize     string `xml:"FontSize,omitempty"`
	FontType     string `xml:"FontType,omitempty"`
	FontColor    string `xml:"FontColor,omitempty"`
	Transparency string `xml:"Transparency,omitempty"`
	Text         string `xml:"Text,omitempty"`
}

type Watermark struct {
	Type      string `xml:"Type,omitempty"`
	Pos       string `xml:"Pos,omitempty"`
	LocMode   string `xml:"LocMode,omitempty"`
	Dx        string `xml:"Dx,omitempty"`
	Dy        string `xml:"Dy,omitempty"`
	StartTime string `xml:"StartTime,omitempty"`
	EndTime   string `xml:"EndTime,omitempty"`
	Image     *Image `xml:"Image,omitempty"`
	Text      *Text  `xml:"Text,omitempty"`
}
type ConcatFragment struct {
	Url       string `xml:"Url,omitempty"`
	StartTime string `xml:"StartTime,omitempty"`
	EndTime   string `xml:"EndTime,omitempty"`
}
type ConcatTemplate struct {
	ConcatFragment []ConcatFragment `xml:"ConcatFragment,omitempty"`
	Audio          *Audio           `xml:"Audio,omitempty"`
	Video          *Video           `xml:"Video,omitempty"`
	Container      *Container       `xml:"Container,omitempty"`
	Index          string           `xml:"Index,omitempty"`
}
type MediaProcessJobOperation struct {
	Output              *JobOutput      `xml:"Output,omitempty"`
	Transcode           *Transcode      `xml:"Transcode,omitempty"`
	Watermark           *Watermark      `xml:"Watermark,omitempty"`
	TemplateId          string          `xml:"TemplateId,omitempty"`
	WatermarkTemplateId []string        `xml:"WatermarkTemplateId,omitempty"`
	ConcatTemplate      *ConcatTemplate `xml:"ConcatTemplate,omitempty"`
}

type CreateMediaJobsOptions struct {
	XMLName   xml.Name                  `xml:"Request"`
	Tag       string                    `xml:"Tag,omitempty"`
	Input     *JobInput                 `xml:"Input,omitempty"`
	Operation *MediaProcessJobOperation `xml:"Operation,omitempty"`
	QueueId   string                    `xml:"QueueId,omitempty"`
	CallBack  string                    `xml:"CallBack,omitempty"`
}

type MediaProcessJobDetail struct {
	Code         string                    `xml:"Code,omitempty"`
	Message      string                    `xml:"Message,omitempty"`
	JobId        string                    `xml:"JobId,omitempty"`
	Tag          string                    `xml:"Tag,omitempty"`
	State        string                    `xml:"State,omitempty"`
	CreationTime string                    `xml:"CreationTime,omitempty"`
	QueueId      string                    `xml:"QueueId,omitempty"`
	Input        *JobInput                 `xml:"Input,omitempty"`
	Operation    *MediaProcessJobOperation `xml:"Operation,omitempty"`
}

type CreateMediaJobsResult struct {
	XMLName    xml.Name              `xml:"Response"`
	JobsDetail MediaProcessJobDetail `xml:"JobsDetail,omitempty"`
}

func (s *CIService) CreateMediaJobs(ctx context.Context, opt *CreateMediaJobsOptions) (*CreateMediaJobsResult, *Response, error) {
	var res CreateMediaJobsResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/jobs",
		method:  http.MethodPost,
		body:    opt,
		result:  &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type DescribeMediaProcessJobResult struct {
	XMLName        xml.Name               `xml:"Response"`
	JobsDetail     *MediaProcessJobDetail `xml:"JobsDetail,omitempty"`
	NonExistJobIds string                 `xml:"NonExistJobIds,omitempty"`
}

func (s *CIService) DescribeMediaJob(ctx context.Context, jobid string) (*DescribeMediaProcessJobResult, *Response, error) {
	var res DescribeMediaProcessJobResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/jobs/" + jobid,
		method:  http.MethodGet,
		result:  &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type DescribeMediaJobsOptions struct {
	QueueId           string `url:"queueId,omitempty"`
	Tag               string `url:"tag,omitempty"`
	OrderByTime       string `url:"orderByTime,omitempty"`
	NextToken         string `url:"nextToken,omitempty"`
	Size              int    `url:"size,omitempty"`
	States            string `url:"states,omitempty"`
	StartCreationTime string `url:"startCreationTime,omitempty"`
	EndCreationTime   string `url:"endCreationTime,omitempty"`
}

type DescribeMediaJobsResult struct {
	XMLName    xml.Name              `xml:"Response"`
	JobsDetail []DocProcessJobDetail `xml:"JobsDetail,omitempty"`
	NextToken  string                `xml:"NextToken,omitempty"`
}

func (s *CIService) DescribeMediaJobs(ctx context.Context, opt *DescribeMediaJobsOptions) (*DescribeMediaJobsResult, *Response, error) {
	var res DescribeMediaJobsResult
	sendOpt := sendOptions{
		baseURL:  s.client.BaseURL.CIURL,
		uri:      "/jobs",
		optQuery: opt,
		method:   http.MethodGet,
		result:   &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type DescribeMediaProcessQueuesOptions struct {
	QueueIds   string `url:"queueIds,omitempty"`
	State      string `url:"state,omitempty"`
	PageNumber int    `url:"pageNumber,omitempty"`
	PageSize   int    `url:"pageSize,omitempty"`
}

type DescribeMediaProcessQueuesResult struct {
	XMLName      xml.Name            `xml:"Response"`
	RequestId    string              `xml:"RequestId,omitempty"`
	TotalCount   int                 `xml:"TotalCount,omitempty"`
	PageNumber   int                 `xml:"PageNumber,omitempty"`
	PageSize     int                 `xml:"PageSize,omitempty"`
	QueueList    []MediaProcessQueue `xml:"QueueList,omitempty"`
	NonExistPIDs []string            `xml:"NonExistPIDs,omitempty"`
}

type MediaProcessQueue struct {
	QueueId       string                         `xml:"QueueId,omitempty"`
	Name          string                         `xml:"Name,omitempty"`
	State         string                         `xml:"State,omitempty"`
	MaxSize       int                            `xml:"MaxSize,omitempty"`
	MaxConcurrent int                            `xml:"MaxConcurrent,omitempty"`
	UpdateTime    string                         `xml:"UpdateTime,omitempty"`
	CreateTime    string                         `xml:"CreateTime,omitempty"`
	NotifyConfig  *MediaProcessQueueNotifyConfig `xml:"NotifyConfig,omitempty"`
}

type MediaProcessQueueNotifyConfig struct {
	Url   string `xml:"Url,omitempty"`
	State string `xml:"State,omitempty"`
	Type  string `xml:"Type,omitempty"`
	Event string `xml:"Event,omitempty"`
}

func (s *CIService) DescribeMediaProcessQueues(ctx context.Context, opt *DescribeMediaProcessQueuesOptions) (*DescribeMediaProcessQueuesResult, *Response, error) {
	var res DescribeMediaProcessQueuesResult
	sendOpt := sendOptions{
		baseURL:  s.client.BaseURL.CIURL,
		uri:      "/queue",
		optQuery: opt,
		method:   http.MethodGet,
		result:   &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type UpdateMediaProcessQueueOptions struct {
	XMLName      xml.Name                       `xml:"Request"`
	Name         string                         `xml:"Name,omitempty"`
	QueueID      string                         `xml:"QueueID,omitempty"`
	State        string                         `xml:"State,omitempty"`
	NotifyConfig *MediaProcessQueueNotifyConfig `xml:"NotifyConfig,omitempty"`
}

type UpdateMediaProcessQueueResult struct {
	XMLName   xml.Name           `xml:"Response"`
	RequestId string             `xml:"RequestId"`
	Queue     *MediaProcessQueue `xml:"Queue"`
}

func (s *CIService) UpdateMediaProcessQueue(ctx context.Context, opt *UpdateMediaProcessQueueOptions) (*UpdateMediaProcessQueueResult, *Response, error) {
	var res UpdateMediaProcessQueueResult
	sendOpt := sendOptions{
		baseURL: s.client.BaseURL.CIURL,
		uri:     "/queue/" + opt.QueueID,
		body:    opt,
		method:  http.MethodPut,
		result:  &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

type DescribeMediaProcessBucketsOptions struct {
	Regions     string `url:"regions,omitempty"`
	BucketNames string `url:"bucketNames,omitempty"`
	BucketName  string `url:"bucketName,omitempty"`
	PageNumber  int    `url:"pageNumber,omitempty"`
	PageSize    int    `url:"pageSize,omitempty"`
}

type DescribeMediaProcessBucketsResult struct {
	XMLName         xml.Name             `xml:"Response"`
	RequestId       string               `xml:"RequestId,omitempty"`
	TotalCount      int                  `xml:"TotalCount,omitempty"`
	PageNumber      int                  `xml:"PageNumber,omitempty"`
	PageSize        int                  `xml:"PageSize,omitempty"`
	MediaBucketList []MediaProcessBucket `xml:"MediaBucketList,omitempty"`
}
type MediaProcessBucket struct {
	BucketId   string `xml:"BucketId,omitempty"`
	Region     string `xml:"Region,omitempty"`
	CreateTime string `xml:"CreateTime,omitempty"`
}

func (s *CIService) DescribeMediaProcessBuckets(ctx context.Context, opt *DescribeMediaProcessBucketsOptions) (*DescribeMediaProcessBucketsResult, *Response, error) {
	var res DescribeMediaProcessBucketsResult
	sendOpt := sendOptions{
		baseURL:  s.client.BaseURL.CIURL,
		uri:      "/mediabucket",
		optQuery: opt,
		method:   http.MethodGet,
		result:   &res,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	return &res, resp, err
}

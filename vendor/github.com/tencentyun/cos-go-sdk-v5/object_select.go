package cos

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"hash/crc32"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"time"
)

type JSONInputSerialization struct {
	Type string `xml:"Type,omitempty"`
}

type CSVInputSerialization struct {
	RecordDelimiter            string `xml:"RecordDelimiter,omitempty"`
	FieldDelimiter             string `xml:"FieldDelimiter,omitempty"`
	QuoteCharacter             string `xml:"QuoteCharacter,omitempty"`
	QuoteEscapeCharacter       string `xml:"QuoteEscapeCharacter,omitempty"`
	AllowQuotedRecordDelimiter string `xml:"AllowQuotedRecordDelimiter,omitempty"`
	FileHeaderInfo             string `xml:"FileHeaderInfo,omitempty"`
	Comments                   string `xml:"Comments,omitempty"`
}

type SelectInputSerialization struct {
	CompressionType string                  `xml:"CompressionType,omitempty"`
	CSV             *CSVInputSerialization  `xml:"CSV,omitempty"`
	JSON            *JSONInputSerialization `xml:"JSON,omitempty"`
}

type JSONOutputSerialization struct {
	RecordDelimiter string `xml:"RecordDelimiter,omitempty"`
}

type CSVOutputSerialization struct {
	QuoteFields          string `xml:"QuoteFields,omitempty"`
	RecordDelimiter      string `xml:"RecordDelimiter,omitempty"`
	FieldDelimiter       string `xml:"FieldDelimiter,omitempty"`
	QuoteCharacter       string `xml:"QuoteCharacter,omitempty"`
	QuoteEscapeCharacter string `xml:"QuoteEscapeCharacter,omitempty"`
}

type SelectOutputSerialization struct {
	CSV  *CSVOutputSerialization  `xml:"CSV,omitempty"`
	JSON *JSONOutputSerialization `xml:"JSON,omitempty"`
}

type ObjectSelectOptions struct {
	XMLName             xml.Name                   `xml:"SelectRequest"`
	Expression          string                     `xml:"Expression"`
	ExpressionType      string                     `xml:"ExpressionType"`
	InputSerialization  *SelectInputSerialization  `xml:"InputSerialization"`
	OutputSerialization *SelectOutputSerialization `xml:"OutputSerialization"`
	RequestProgress     string                     `xml:"RequestProgress>Enabled,omitempty"`
}

func (s *ObjectService) Select(ctx context.Context, name string, opt *ObjectSelectOptions) (io.ReadCloser, error) {
	u := fmt.Sprintf("/%s?select&select-type=2", encodeURIComponent(name))
	sendOpt := sendOptions{
		baseURL:          s.client.BaseURL.BucketURL,
		uri:              u,
		method:           http.MethodPost,
		body:             opt,
		disableCloseBody: true,
	}
	resp, err := s.client.send(ctx, &sendOpt)
	if err != nil {
		return nil, err
	}
	result := &ObjectSelectResponse{
		Headers:    resp.Header,
		Body:       resp.Body,
		StatusCode: resp.StatusCode,
		Frame: &ObjectSelectResult{
			NextFrame: true,
			Payload:   []byte{},
		},
		Finish: false,
	}

	return result, nil
}

func (s *ObjectService) SelectToFile(ctx context.Context, name, file string, opt *ObjectSelectOptions) (*ObjectSelectResponse, error) {
	resp, err := s.Select(ctx, name, opt)
	if err != nil {
		return nil, err
	}
	res, _ := resp.(*ObjectSelectResponse)
	defer func() {
		io.Copy(ioutil.Discard, resp)
		resp.Close()
	}()

	fd, err := os.OpenFile(file, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(0664))
	if err != nil {
		return res, err
	}

	_, err = io.Copy(fd, resp)
	fd.Close()
	res.Finish = true
	return res, err
}

const (
	kReadTimeout = 3
	kMessageType = ":message-type"
	kEventType   = ":event-type"
	kContentType = ":content-type"

	kRecordsFrameType = iota
	kContinuationFrameType
	kProgressFrameType
	kStatsFrameType
	kEndFrameType
	kErrorFrameType
)

type ProgressFrame struct {
	XMLName        xml.Name `xml:"Progress"`
	BytesScanned   int      `xml:"BytesScanned"`
	BytesProcessed int      `xml:"BytesProcessed"`
	BytesReturned  int      `xml:"BytesReturned"`
}

type StatsFrame struct {
	XMLName        xml.Name `xml:"Stats"`
	BytesScanned   int      `xml:"BytesScanned"`
	BytesProcessed int      `xml:"BytesProcessed"`
	BytesReturned  int      `xml:"BytesReturned"`
}

type DataFrame struct {
	ContentType         string
	ConsumedBytesLength int32
	LeftBytesLength     int32
}

type ErrorFrame struct {
	Code    string
	Message string
}

func (e *ErrorFrame) Error() string {
	return fmt.Sprintf("Error Code: %s, Error Message: %s", e.Code, e.Message)
}

type ObjectSelectResult struct {
	TotalFrameLength  int32
	TotalHeaderLength int32
	NextFrame         bool
	FrameType         int
	Payload           []byte
	DataFrame         DataFrame
	ProgressFrame     ProgressFrame
	StatsFrame        StatsFrame
	ErrorFrame        *ErrorFrame
}

type ObjectSelectResponse struct {
	StatusCode int
	Headers    http.Header
	Body       io.ReadCloser
	Frame      *ObjectSelectResult
	Finish     bool
}

func (osr *ObjectSelectResponse) Read(p []byte) (n int, err error) {
	n, err = osr.readFrames(p)
	return
}
func (osr *ObjectSelectResponse) Close() error {
	return osr.Body.Close()
}

func (osr *ObjectSelectResponse) readFrames(p []byte) (int, error) {
	if osr.Finish {
		return 0, io.EOF
	}
	if osr.Frame.ErrorFrame != nil {
		return 0, osr.Frame.ErrorFrame
	}

	var err error
	var nlen int
	dlen := len(p)

	for nlen < dlen {
		if osr.Frame.NextFrame == true {
			osr.Frame.NextFrame = false
			err := osr.analysisPrelude()
			if err != nil {
				return nlen, err
			}
			err = osr.analysisHeader()
			if err != nil {
				return nlen, err
			}
		}
		switch osr.Frame.FrameType {
		case kRecordsFrameType:
			n, err := osr.analysisRecords(p[nlen:])
			if err != nil {
				return nlen, err
			}
			nlen += n
		case kContinuationFrameType:
			err = osr.payloadChecksum("ContinuationFrame")
			if err != nil {
				return nlen, err
			}
		case kProgressFrameType:
			err := osr.analysisXml(&osr.Frame.ProgressFrame)
			if err != nil {
				return nlen, err
			}
		case kStatsFrameType:
			err := osr.analysisXml(&osr.Frame.StatsFrame)
			if err != nil {
				return nlen, err
			}
		case kEndFrameType:
			err = osr.payloadChecksum("EndFrame")
			if err != nil {
				return nlen, err
			}
			osr.Finish = true
			return nlen, io.EOF
		case kErrorFrameType:
			return nlen, osr.Frame.ErrorFrame
		}
	}
	return nlen, err
}

func (osr *ObjectSelectResponse) analysisPrelude() error {
	frame := make([]byte, 12)
	_, err := osr.fixedLengthRead(frame, kReadTimeout)
	if err != nil {
		return err
	}

	var preludeCRC uint32
	bytesToInt(frame[0:4], &osr.Frame.TotalFrameLength)
	bytesToInt(frame[4:8], &osr.Frame.TotalHeaderLength)
	bytesToInt(frame[8:12], &preludeCRC)
	osr.Frame.Payload = append(osr.Frame.Payload, frame...)

	return checksum(frame[0:8], preludeCRC, "Prelude")
}

func (osr *ObjectSelectResponse) analysisHeader() error {
	var nlen int32
	headers := make(map[string]string)
	for nlen < osr.Frame.TotalHeaderLength {
		var headerNameLen int8
		var headerValueLen int16
		bHeaderNameLen := make([]byte, 1)
		_, err := osr.fixedLengthRead(bHeaderNameLen, kReadTimeout)
		if err != nil {
			return err
		}
		nlen += 1
		bytesToInt(bHeaderNameLen, &headerNameLen)
		osr.Frame.Payload = append(osr.Frame.Payload, bHeaderNameLen...)

		bHeaderName := make([]byte, headerNameLen)
		_, err = osr.fixedLengthRead(bHeaderName, kReadTimeout)
		if err != nil {
			return err
		}
		nlen += int32(headerNameLen)
		headerName := string(bHeaderName)
		osr.Frame.Payload = append(osr.Frame.Payload, bHeaderName...)

		bValueTypeLen := make([]byte, 3)
		_, err = osr.fixedLengthRead(bValueTypeLen, kReadTimeout)
		if err != nil {
			return err
		}
		nlen += 3
		bytesToInt(bValueTypeLen[1:], &headerValueLen)
		osr.Frame.Payload = append(osr.Frame.Payload, bValueTypeLen...)

		bHeaderValue := make([]byte, headerValueLen)
		_, err = osr.fixedLengthRead(bHeaderValue, kReadTimeout)
		if err != nil {
			return err
		}
		nlen += int32(headerValueLen)
		headers[headerName] = string(bHeaderValue)
		osr.Frame.Payload = append(osr.Frame.Payload, bHeaderValue...)
	}
	htype, ok := headers[kMessageType]
	if !ok {
		return fmt.Errorf("header parse failed, no message-type, headers: %+v\n", headers)
	}
	switch {
	case htype == "error":
		osr.Frame.FrameType = kErrorFrameType
		osr.Frame.ErrorFrame = &ErrorFrame{}
		osr.Frame.ErrorFrame.Code, _ = headers[":error-code"]
		osr.Frame.ErrorFrame.Message, _ = headers[":error-message"]
	case htype == "event":
		hevent, ok := headers[kEventType]
		if !ok {
			return fmt.Errorf("header parse failed, no event-type, headers: %+v\n", headers)
		}
		switch {
		case hevent == "Records":
			hContentType, ok := headers[kContentType]
			if ok {
				osr.Frame.DataFrame.ContentType = hContentType
			}
			osr.Frame.FrameType = kRecordsFrameType
		case hevent == "Cont":
			osr.Frame.FrameType = kContinuationFrameType
		case hevent == "Progress":
			osr.Frame.FrameType = kProgressFrameType
		case hevent == "Stats":
			osr.Frame.FrameType = kStatsFrameType
		case hevent == "End":
			osr.Frame.FrameType = kEndFrameType
		default:
			return fmt.Errorf("header parse failed, invalid event-type, headers: %+v\n", headers)
		}
	default:
		return fmt.Errorf("header parse failed, invalid message-type: headers: %+v\n", headers)
	}
	return nil
}

func (osr *ObjectSelectResponse) analysisRecords(data []byte) (int, error) {
	var needReadLength int32
	dlen := int32(len(data))
	restLen := osr.Frame.TotalFrameLength - 16 - osr.Frame.TotalHeaderLength - osr.Frame.DataFrame.ConsumedBytesLength
	if dlen <= restLen {
		needReadLength = dlen
	} else {
		needReadLength = restLen
	}
	n, err := osr.fixedLengthRead(data[:needReadLength], kReadTimeout)
	if err != nil {
		return n, fmt.Errorf("read data frame error: %s", err.Error())
	}
	osr.Frame.DataFrame.ConsumedBytesLength += int32(n)
	osr.Frame.Payload = append(osr.Frame.Payload, data[:needReadLength]...)
	// 读完了一帧数据并填充到data中了
	if osr.Frame.DataFrame.ConsumedBytesLength == osr.Frame.TotalFrameLength-16-osr.Frame.TotalHeaderLength {
		osr.Frame.DataFrame.ConsumedBytesLength = 0
		err = osr.payloadChecksum("RecordFrame")
	}
	return n, err
}

func (osr *ObjectSelectResponse) analysisXml(frame interface{}) error {
	payloadLength := osr.Frame.TotalFrameLength - 16 - osr.Frame.TotalHeaderLength
	bFrame := make([]byte, payloadLength)
	_, err := osr.fixedLengthRead(bFrame, kReadTimeout)
	if err != nil {
		return err
	}
	err = xml.Unmarshal(bFrame, frame)
	if err != nil {
		return err
	}
	osr.Frame.Payload = append(osr.Frame.Payload, bFrame...)
	return osr.payloadChecksum("XmlFrame")
}

// 调用payloadChecksum时，表示该帧已读完，开始读取下一帧内容
func (osr *ObjectSelectResponse) payloadChecksum(ftype string) error {
	bcrc := make([]byte, 4)
	_, err := osr.fixedLengthRead(bcrc, kReadTimeout)
	if err != nil {
		return err
	}
	var res uint32
	bytesToInt(bcrc, &res)
	err = checksum(osr.Frame.Payload, res, ftype)

	osr.Frame.NextFrame = true
	osr.Frame.Payload = []byte{}

	return err
}

type chanReadIO struct {
	readLen int
	err     error
}

func (osr *ObjectSelectResponse) fixedLengthRead(p []byte, read_timeout int64) (int, error) {
	timeout := time.Duration(read_timeout)
	r := osr.Body
	ch := make(chan chanReadIO, 1)
	go func(p []byte) {
		var needLen int
		readChan := chanReadIO{}
		needLen = len(p)
		for {
			n, err := r.Read(p[readChan.readLen:needLen])
			readChan.readLen += n
			if err != nil {
				readChan.err = err
				ch <- readChan
				close(ch)
				return
			}

			if readChan.readLen == needLen {
				break
			}
		}
		ch <- readChan
		close(ch)
	}(p)

	select {
	case <-time.After(time.Second * timeout):
		return 0, fmt.Errorf("requestId: %s, readLen timeout, timeout is %d(second),need read:%d", osr.Headers.Get("x-cos-request-id"), timeout, len(p))
	case result := <-ch:
		return result.readLen, result.err
	}
}

func bytesToInt(b []byte, ret interface{}) {
	binBuf := bytes.NewBuffer(b)
	binary.Read(binBuf, binary.BigEndian, ret)
}

func checksum(b []byte, rec uint32, ftype string) error {
	c := crc32.ChecksumIEEE(b)
	if c != rec {
		return fmt.Errorf("parse type: %v, checksum failed, cal: %v, rec: %v\n", ftype, c, rec)
	}
	return nil
}

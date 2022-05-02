package cos

import (
	"fmt"
	"hash"
	"io"
)

type ProgressEventType int

const (
	// 数据开始传输
	ProgressStartedEvent ProgressEventType = iota
	// 数据传输中
	ProgressDataEvent
	// 数据传输完成, 但不能表示对应API调用完成
	ProgressCompletedEvent
	// 只有在数据传输时发生错误才会返回
	ProgressFailedEvent
)

type ProgressEvent struct {
	EventType     ProgressEventType
	RWBytes       int64
	ConsumedBytes int64
	TotalBytes    int64
	Err           error
}

func newProgressEvent(eventType ProgressEventType, rwBytes, consumed, total int64, err ...error) *ProgressEvent {
	event := &ProgressEvent{
		EventType:     eventType,
		RWBytes:       rwBytes,
		ConsumedBytes: consumed,
		TotalBytes:    total,
	}
	if len(err) > 0 {
		event.Err = err[0]
	}
	return event
}

// 用户自定义Listener需要实现该方法
type ProgressListener interface {
	ProgressChangedCallback(event *ProgressEvent)
}

func progressCallback(listener ProgressListener, event *ProgressEvent) {
	if listener != nil && event != nil {
		listener.ProgressChangedCallback(event)
	}
}

type teeReader struct {
	reader          io.Reader
	writer          io.Writer
	consumedBytes   int64
	totalBytes      int64
	listener        ProgressListener
	disableCheckSum bool
}

func (r *teeReader) Read(p []byte) (int, error) {
	if r.consumedBytes == 0 {
		event := newProgressEvent(ProgressStartedEvent, 0, r.consumedBytes, r.totalBytes)
		progressCallback(r.listener, event)
	}

	n, err := r.reader.Read(p)
	if err != nil && err != io.EOF {
		event := newProgressEvent(ProgressFailedEvent, 0, r.consumedBytes, r.totalBytes, err)
		progressCallback(r.listener, event)
	}
	if n > 0 {
		r.consumedBytes += int64(n)
		if r.writer != nil {
			if n, err := r.writer.Write(p[:n]); err != nil {
				return n, err
			}
		}
		if r.listener != nil {
			event := newProgressEvent(ProgressDataEvent, int64(n), r.consumedBytes, r.totalBytes)
			progressCallback(r.listener, event)
		}
	}

	if err == io.EOF {
		event := newProgressEvent(ProgressCompletedEvent, int64(n), r.consumedBytes, r.totalBytes)
		progressCallback(r.listener, event)
	}

	return n, err
}

func (r *teeReader) Close() error {
	if rc, ok := r.reader.(io.ReadCloser); ok {
		return rc.Close()
	}
	return nil
}

func (r *teeReader) Size() int64 {
	return r.totalBytes
}

func (r *teeReader) Crc64() uint64 {
	if r.writer != nil {
		if th, ok := r.writer.(hash.Hash64); ok {
			return th.Sum64()
		}
	}
	return 0
}

func (r *teeReader) Sum() []byte {
	if r.writer != nil {
		if th, ok := r.writer.(hash.Hash); ok {
			return th.Sum(nil)
		}
	}
	return []byte{}
}

func TeeReader(reader io.Reader, writer io.Writer, total int64, listener ProgressListener) *teeReader {
	return &teeReader{
		reader:          reader,
		writer:          writer,
		consumedBytes:   0,
		totalBytes:      total,
		listener:        listener,
		disableCheckSum: false,
	}
}

type FixedLengthReader interface {
	io.Reader
	Size() int64
}

type DefaultProgressListener struct {
}

func (l *DefaultProgressListener) ProgressChangedCallback(event *ProgressEvent) {
	switch event.EventType {
	case ProgressStartedEvent:
		fmt.Printf("Transfer Start    [ConsumedBytes/TotalBytes: %d/%d]\n",
			event.ConsumedBytes, event.TotalBytes)
	case ProgressDataEvent:
		fmt.Printf("\rTransfer Data     [ConsumedBytes/TotalBytes: %d/%d, %d%%]",
			event.ConsumedBytes, event.TotalBytes, event.ConsumedBytes*100/event.TotalBytes)
	case ProgressCompletedEvent:
		fmt.Printf("\nTransfer Complete [ConsumedBytes/TotalBytes: %d/%d]\n",
			event.ConsumedBytes, event.TotalBytes)
	case ProgressFailedEvent:
		fmt.Printf("\nTransfer Failed   [ConsumedBytes/TotalBytes: %d/%d] [Err: %v]\n",
			event.ConsumedBytes, event.TotalBytes, event.Err)
	default:
		fmt.Printf("Progress Changed Error: unknown progress event type\n")
	}
}

package utils

// ProgressEventType defines transfer progress event type
type ProgressEventType int

const (
	// TransferStartedEvent transfer started, set TotalBytes
	TransferStartedEvent ProgressEventType = 1 + iota
	// TransferDataEvent transfer data, set ConsumedBytes anmd TotalBytes
	TransferDataEvent
	// TransferCompletedEvent transfer completed
	TransferCompletedEvent
	// TransferFailedEvent transfer encounters an error
	TransferFailedEvent
)

// ProgressEvent defines progress event
type ProgressEvent struct {
	ConsumedBytes int64
	TotalBytes    int64
	RwBytes       int64
	EventType     ProgressEventType
}

// ProgressListener listens progress change
type ProgressListener interface {
	ProgressChanged(event *ProgressEvent)
}

// -------------------- Private --------------------

func NewProgressEvent(eventType ProgressEventType, consumed, total int64, rwBytes int64) *ProgressEvent {
	return &ProgressEvent{
		ConsumedBytes: consumed,
		TotalBytes:    total,
		RwBytes:       rwBytes,
		EventType:     eventType}
}

// publishProgress
func PublishProgress(listener ProgressListener, event *ProgressEvent) {
	if listener != nil && event != nil {
		listener.ProgressChanged(event)
	}
}

func GetProgressListener(obj interface{}) ProgressListener {
	if obj == nil {
		return nil
	}
	listener, ok := obj.(ProgressListener)
	if !ok {
		return nil
	}
	return listener
}

type ReaderTracker struct {
	CompletedBytes int64
}

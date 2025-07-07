package notification

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"cloud.google.com/go/pubsub"
	"github.com/fsouza/fake-gcs-server/internal/backend"
)

// EventType is the type of event to trigger. The descriptions of the events
// can be found here:
// https://cloud.google.com/storage/docs/pubsub-notifications#events.
type EventType string

const (
	// EventFinalize is triggered when an object is added.
	EventFinalize EventType = "OBJECT_FINALIZE"
	// EventDelete is triggered when an object is deleted.
	EventDelete = "OBJECT_DELETE"
	// EventMetadata is triggered when an object's metadata is changed.
	EventMetadata = "OBJECT_METADATA_UPDATE"
	// EventArchive bucket versioning must be enabled. is triggered when an object becomes the non current version
	EventArchive = "OBJECT_ARCHIVE"
)

// EventNotificationOptions contains flags for events, that if true, will create
// trigger notifications when they occur.
type EventNotificationOptions struct {
	Finalize       bool
	Delete         bool
	MetadataUpdate bool
	Archive        bool
}

// EventManagerOptions determines what events are triggered and where.
type EventManagerOptions struct {
	// ProjectID is the project ID containing the pubsub topic.
	ProjectID string
	// TopicName is the pubsub topic name to publish events on.
	TopicName string
	// Bucket is the name of the bucket to publish events from.
	Bucket string
	// ObjectPrefix, if not empty, only objects having this prefix will generate
	// trigger events.
	ObjectPrefix string
	// NotifyOn determines what events to trigger.
	NotifyOn EventNotificationOptions
}

type EventManager interface {
	Trigger(o *backend.StreamingObject, eventType EventType, extraEventAttr map[string]string)
}

// PubsubEventManager checks if an event should be published.
type PubsubEventManager struct {
	// publishSynchronously is a flag that if true, events will be published
	// synchronously and not in a goroutine. It is used during tests to prevent
	// race conditions.
	publishSynchronously bool
	// notifyOn determines what events are triggered.
	notifyOn EventNotificationOptions
	// writer is where logs are written to.
	writer io.Writer
	// bucket, if not empty, only objects from this bucket will generate trigger events.
	bucket string
	// objectPrefix, if not empty, only objects having this prefix will generate
	// trigger events.
	objectPrefix string
	//  publisher is used to publish events on.
	publisher eventPublisher
}

func NewPubsubEventManager(options EventManagerOptions, w io.Writer) (*PubsubEventManager, error) {
	manager := &PubsubEventManager{
		writer:       w,
		notifyOn:     options.NotifyOn,
		bucket:       options.Bucket,
		objectPrefix: options.ObjectPrefix,
	}
	if options.ProjectID != "" && options.TopicName != "" {
		ctx := context.Background()
		client, err := pubsub.NewClient(ctx, options.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("error creating pubsub client: %v", err)
		}
		manager.publisher = client.Topic(options.TopicName)
	}
	return manager, nil
}

// eventPublisher is the interface to publish triggered events.
type eventPublisher interface {
	Publish(ctx context.Context, msg *pubsub.Message) *pubsub.PublishResult
}

// Trigger checks if an event should be triggered. If so, it publishes the
// event to a pubsub queue.
func (m *PubsubEventManager) Trigger(o *backend.StreamingObject, eventType EventType, extraEventAttr map[string]string) {
	if m.publisher == nil {
		return
	}
	if m.bucket != "" && o.BucketName != m.bucket {
		return
	}
	if m.objectPrefix != "" && !strings.HasPrefix(o.Name, m.objectPrefix) {
		return
	}
	switch eventType {
	case EventFinalize:
		if !m.notifyOn.Finalize {
			return
		}
	case EventDelete:
		if !m.notifyOn.Delete {
			return
		}
	case EventMetadata:
		if !m.notifyOn.MetadataUpdate {
			return
		}
	case EventArchive:
		if !m.notifyOn.Archive {
			return
		}
	}
	eventTime := time.Now().Format(time.RFC3339)
	publishFunc := func() {
		err := m.publish(o, eventType, eventTime, extraEventAttr)
		if m.writer != nil {
			if err != nil {
				fmt.Fprintf(m.writer, "error publishing event: %v", err)
			} else {
				fmt.Fprintf(m.writer, "sent event %s for object %s\n", string(eventType), o.ID())
			}
		}
	}
	if m.publishSynchronously {
		publishFunc()
	} else {
		go publishFunc()
	}
}

func (m *PubsubEventManager) publish(o *backend.StreamingObject, eventType EventType, eventTime string, extraEventAttr map[string]string) error {
	ctx := context.Background()
	data, attributes, err := generateEvent(o, eventType, eventTime, extraEventAttr)
	if err != nil {
		return err
	}
	if r := m.publisher.Publish(ctx, &pubsub.Message{
		Data:       data,
		Attributes: attributes,
	}); r != nil {
		_, err = r.Get(ctx)
		return err
	}
	return nil
}

// gcsEvent is the payload of a GCS event. Note that all properties are string-quoted.
// The description of the full object can be found here:
// https://cloud.google.com/storage/docs/json_api/v1/objects#resource-representations.
type gcsEvent struct {
	Kind            string            `json:"kind"`
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Bucket          string            `json:"bucket"`
	Generation      int64             `json:"generation,string,omitempty"`
	ContentType     string            `json:"contentType"`
	ContentEncoding string            `json:"contentEncoding,omitempty"`
	Created         string            `json:"timeCreated,omitempty"`
	Updated         string            `json:"updated,omitempty"`
	StorageClass    string            `json:"storageClass"`
	Size            int64             `json:"size,string"`
	MD5Hash         string            `json:"md5Hash,omitempty"`
	CRC32c          string            `json:"crc32c,omitempty"`
	MetaData        map[string]string `json:"metadata,omitempty"`
}

func generateEvent(o *backend.StreamingObject, eventType EventType, eventTime string, extraEventAttr map[string]string) ([]byte, map[string]string, error) {
	payload := gcsEvent{
		Kind:            "storage#object",
		ID:              o.ID(),
		Name:            o.Name,
		Bucket:          o.BucketName,
		Generation:      o.Generation,
		ContentType:     o.ContentType,
		ContentEncoding: o.ContentEncoding,
		Created:         o.Created,
		Updated:         o.Updated,
		StorageClass:    "STANDARD",
		Size:            o.Size,
		MD5Hash:         o.Md5Hash,
		CRC32c:          o.Crc32c,
		MetaData:        o.Metadata,
	}
	attributes := map[string]string{
		"bucketId":         o.BucketName,
		"eventTime":        eventTime,
		"eventType":        string(eventType),
		"objectGeneration": strconv.FormatInt(o.Generation, 10),
		"objectId":         o.Name,
		"payloadFormat":    "JSON_API_V1",
	}
	for k, v := range extraEventAttr {
		if _, exists := attributes[k]; exists {
			return nil, nil, fmt.Errorf("cannot overwrite duplicate event attribute %s", k)
		}
		attributes[k] = v
	}
	data, err := json.Marshal(&payload)
	if err != nil {
		return nil, nil, err
	}
	return data, attributes, nil
}

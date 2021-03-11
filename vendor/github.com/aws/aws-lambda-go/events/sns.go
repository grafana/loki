// Copyright 2017 Amazon.com, Inc. or its affiliates. All Rights Reserved.

package events

import (
	"time"
)

type SNSEvent struct {
	Records []SNSEventRecord `json:"Records"`
}

type SNSEventRecord struct {
	EventVersion         string    `json:"EventVersion"`
	EventSubscriptionArn string    `json:"EventSubscriptionArn"`
	EventSource          string    `json:"EventSource"`
	SNS                  SNSEntity `json:"Sns"`
}

type SNSEntity struct {
	Signature         string                 `json:"Signature"`
	MessageID         string                 `json:"MessageId"`
	Type              string                 `json:"Type"`
	TopicArn          string                 `json:"TopicArn"`
	MessageAttributes map[string]interface{} `json:"MessageAttributes"`
	SignatureVersion  string                 `json:"SignatureVersion"`
	Timestamp         time.Time              `json:"Timestamp"`
	SigningCertURL    string                 `json:"SigningCertUrl"`
	Message           string                 `json:"Message"`
	UnsubscribeURL    string                 `json:"UnsubscribeUrl"`
	Subject           string                 `json:"Subject"`
}

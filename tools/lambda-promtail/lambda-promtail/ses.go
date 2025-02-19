package main

import (
	"context"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/prometheus/common/model"
)

// SESNotification represents the SES-specific message structure
type SESNotification struct {
	NotificationType string     `json:"notificationType"`
	Mail             Mail       `json:"mail"`
	Bounce           *Bounce    `json:"bounce,omitempty"`
	Complaint        *Complaint `json:"complaint,omitempty"`
	Delivery         *Delivery  `json:"delivery,omitempty"`
}

// Mail represents the email details
type Mail struct {
	Timestamp        string            `json:"timestamp"`
	Source           string            `json:"source"`
	Destination      []string          `json:"destination"`
	Headers          []Header          `json:"headers"`
	CommonHeaders    CommonHeaders     `json:"commonHeaders"`
	MessageId        string            `json:"messageId"`
	SourceArn        string            `json:"sourceArn"`
	SendingAccountId string            `json:"sendingAccountId"`
	Tags             map[string]string `json:"tags"`
}

// DeliveryMessage represents the structure of an SES delivery notification.
type DeliveryMessage struct {
	NotificationType string `json:"notificationType"`
	Delivery         struct {
		Timestamp            string            `json:"timestamp"`
		ProcessingTimeMillis int               `json:"processingTimeMillis"`
		Recipients           []string          `json:"recipients"`
		DsnFields            map[string]string `json:"dnsFields"`
		ReportingMTA         string            `json:"reportingMTA"`
		SmtpResponse         string            `json:"smtpResponse"`
		RemoteHost           string            `json:"remoteHost"`
	} `json:"delivery"`
	Mail struct {
		Timestamp        string              `json:"timestamp"`
		Source           string              `json:"source"`
		SourceArn        string              `json:"sourceArn"`
		SendingAccountId string              `json:"sendingAccountId"`
		MessageId        string              `json:"messageId"`
		Destination      []string            `json:"destination"`
		Headers          []map[string]string `json:"headers"`
		CommonHeaders    map[string]string   `json:"commonHeaders"`
		Tags             map[string][]struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"tags"`
	} `json:"mail"`
}

// Header represents an email header
type Header struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// CommonHeaders represents common email headers
type CommonHeaders struct {
	From      []string `json:"from"`
	To        []string `json:"to"`
	Subject   string   `json:"subject"`
	MessageId string   `json:"messageId"`
}

// Bounce represents the bounce details
type Bounce struct {
	Timestamp         string      `json:"timestamp"`
	FeedbackId        string      `json:"feedbackId"`
	BouncedRecipients []Recipient `json:"bouncedRecipients"`
	ReportingMTA      string      `json:"reportingMTA"`
	BounceType        string      `json:"bounceType"`
	BounceSubType     string      `json:"bounceSubType"`
}

type Recipient struct {
	EmailAddress   string `json:"emailAddress"`
	Action         string `json:"action"`
	Status         string `json="status"`
	DiagnosticCode string `json:"diagnosticCode"`
}

// Complaint represents complaint details
type Complaint struct {
	UserAgent            string                `json:"userAgent"`
	Type                 string                `json:"complaintFeedbackType"`
	Timestamp            string                `json:"timestamp"`
	FeedbackId           string                `json:"feedbackId"`
	ArrivalDate          string                `json:"arrivalDate"`
	ComplainedRecipients []ComplainedRecipient `json:"complainedRecipients"`
}

// ComplainedRecipient represents details of a recipient who complained
type ComplainedRecipient struct {
	EmailAddress string `json:"emailAddress"`
}

// Delivery represents delivery details
type Delivery struct {
	Timestamp            string   `json:"timestamp"`
	ProcessingTimeMillis int      `json:"processingTimeMillis"`
	Recipients           []string `json:"recipients"`
	SmtpResponse         string   `json:"smtpResponse"`
	RemoteMtaIp          string   `json:"remoteMtaIp"`
	ReportingMTA         string   `json:"reportingMTA"`
}

func parseSESEvent(ctx context.Context, b *batch, log *log.Logger, ev *SESNotification) error {
	labels := model.LabelSet{
		model.LabelName("__aws_log_type"): model.LabelValue("SQS - SES"),
		model.LabelName("__aws_ses_type"): model.LabelValue(ev.NotificationType),
		model.LabelName("service_name"):   model.LabelValue("AWS - SES"),
	}

	level.Debug(*log).Log("msg", fmt.Sprintf("Processing SES Event"))
	if ev.Bounce != nil {
		// Message is of type Bounce
		level.Debug(*log).Log("msg", fmt.Sprintf("Processing SES Event as type Bounce"))

		timestamp, _ := time.Parse(time.RFC3339, ev.Bounce.Timestamp)

		beventLabels := model.LabelSet{
			model.LabelName("bounce_type"):     model.LabelValue(ev.Bounce.BounceType),
			model.LabelName("bounce_sub_type"): model.LabelValue(ev.Bounce.BounceSubType),
		}
		bventLabels := model.LabelSet.Merge(labels, beventLabels)
		for _, recipent := range ev.Bounce.BouncedRecipients {
			msg := "Failed to deliever to recipent: '" + recipent.EmailAddress
			msg = msg + "' with action: '" + recipent.Action
			msg = msg + "' and status '" + recipent.Status
			msg = msg + "' and diagnosticCode '" + recipent.DiagnosticCode
			msg = msg + "' " + fmt.Sprintf("%+v", ev.Mail)

			recipentLabels := model.LabelSet{
				model.LabelName("email_address"): model.LabelValue(recipent.EmailAddress),
			}

			recipentEventLabels := model.LabelSet.Merge(bventLabels, recipentLabels)
			if batchErr := b.add(ctx, entry{recipentEventLabels, logproto.Entry{
				Line:      msg,
				Timestamp: timestamp,
			}}); batchErr != nil {
				return batchErr
			}
		}
	}
	if ev.Complaint != nil {
		// Message is of type complaint
		level.Debug(*log).Log("msg", fmt.Sprintf("Processing SES Event as type Complaint"))
		timestamp, _ := time.Parse(time.RFC3339, ev.Complaint.Timestamp)

		ceventLabels := model.LabelSet{
			model.LabelName("complaint_type"): model.LabelValue(ev.Complaint.Type),
		}
		cventLabels := model.LabelSet.Merge(labels, ceventLabels)
		for _, recipent := range ev.Complaint.ComplainedRecipients {
			msg := "Received a complaint from recipent: '" + recipent.EmailAddress
			msg = msg + "' "
			msg = msg + fmt.Sprintf("%+v", ev.Mail)
			recipentLabels := model.LabelSet{
				model.LabelName("email_address"): model.LabelValue(recipent.EmailAddress),
			}
			recipentEventLabels := model.LabelSet.Merge(cventLabels, recipentLabels)
			if batchErr := b.add(ctx, entry{recipentEventLabels, logproto.Entry{
				Line:      msg,
				Timestamp: timestamp,
			}}); batchErr != nil {
				return batchErr
			}
		}
	}
	if ev.Delivery != nil {
		// Message is of type delivery
		level.Debug(*log).Log("msg", fmt.Sprintf("Processing SES Event as type Delivery"))
		timestamp, _ := time.Parse(time.RFC3339, ev.Delivery.Timestamp)

		if batchErr := b.add(ctx, entry{labels, logproto.Entry{
			Line:      fmt.Sprintf("%+v", ev.Mail),
			Timestamp: timestamp,
		}}); batchErr != nil {
			return batchErr
		}
	}
	return nil // TODOfix
}

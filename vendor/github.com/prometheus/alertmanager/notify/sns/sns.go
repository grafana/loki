// Copyright 2021 Prometheus Team
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package sns

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/credentials/stscreds"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/sns"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/notify"
	"github.com/prometheus/alertmanager/template"
	"github.com/prometheus/alertmanager/types"
	commoncfg "github.com/prometheus/common/config"
)

// Notifier implements a Notifier for SNS notifications.
type Notifier struct {
	conf    *config.SNSConfig
	tmpl    *template.Template
	logger  log.Logger
	client  *http.Client
	retrier *notify.Retrier
	isFifo  *bool
}

// New returns a new SNS notification handler.
func New(c *config.SNSConfig, t *template.Template, l log.Logger, httpOpts ...commoncfg.HTTPClientOption) (*Notifier, error) {
	client, err := commoncfg.NewClientFromConfig(*c.HTTPConfig, "sns", append(httpOpts, commoncfg.WithHTTP2Disabled())...)
	if err != nil {
		return nil, err
	}
	return &Notifier{
		conf:    c,
		tmpl:    t,
		logger:  l,
		client:  client,
		retrier: &notify.Retrier{},
	}, nil
}

func (n *Notifier) Notify(ctx context.Context, alert ...*types.Alert) (bool, error) {
	var (
		err  error
		data = notify.GetTemplateData(ctx, n.tmpl, alert, n.logger)
		tmpl = notify.TmplText(n.tmpl, data, &err)
	)

	client, err := createSNSClient(n, tmpl)
	if err != nil {
		if e, ok := err.(awserr.RequestFailure); ok {
			return n.retrier.Check(e.StatusCode(), strings.NewReader(e.Message()))
		} else {
			return true, err
		}
	}

	publishInput, err := createPublishInput(ctx, n, tmpl)
	if err != nil {
		return true, err
	}

	publishOutput, err := client.Publish(publishInput)
	if err != nil {
		if e, ok := err.(awserr.RequestFailure); ok {
			return n.retrier.Check(e.StatusCode(), strings.NewReader(e.Message()))
		} else {
			return true, err
		}
	}

	level.Debug(n.logger).Log("msg", "SNS message successfully published", "message_id", publishOutput.MessageId, "sequence number", publishOutput.SequenceNumber)

	return false, nil
}

func createSNSClient(n *Notifier, tmpl func(string) string) (*sns.SNS, error) {
	var creds *credentials.Credentials = nil
	// If there are provided sigV4 credentials we want to use those to create a session.
	if n.conf.Sigv4.AccessKey != "" && n.conf.Sigv4.SecretKey != "" {
		creds = credentials.NewStaticCredentials(n.conf.Sigv4.AccessKey, string(n.conf.Sigv4.SecretKey), "")
	}
	sess, err := session.NewSessionWithOptions(session.Options{
		Config: aws.Config{
			Region:   aws.String(n.conf.Sigv4.Region),
			Endpoint: aws.String(tmpl(n.conf.APIUrl)),
		},
		Profile: n.conf.Sigv4.Profile,
	})
	if err != nil {
		return nil, err
	}

	if n.conf.Sigv4.RoleARN != "" {
		var stsSess *session.Session
		if n.conf.APIUrl == "" {
			stsSess = sess
		} else {
			// If we have set the API URL we need to create a new session to get the STS Credentials.
			stsSess, err = session.NewSessionWithOptions(session.Options{
				Config: aws.Config{
					Region:      aws.String(n.conf.Sigv4.Region),
					Credentials: creds,
				},
				Profile: n.conf.Sigv4.Profile,
			})
			if err != nil {
				return nil, err
			}
		}
		creds = stscreds.NewCredentials(stsSess, n.conf.Sigv4.RoleARN)
	}
	// Use our generated session with credentials to create the SNS Client.
	client := sns.New(sess, &aws.Config{Credentials: creds})
	// We will always need a region to be set by either the local config or the environment.
	if aws.StringValue(sess.Config.Region) == "" {
		return nil, fmt.Errorf("region not configured in sns.sigv4.region or in default credentials chain")
	}
	return client, nil
}

func createPublishInput(ctx context.Context, n *Notifier, tmpl func(string) string) (*sns.PublishInput, error) {
	publishInput := &sns.PublishInput{}
	messageAttributes := createMessageAttributes(n, tmpl)
	// Max message size for a message in a SNS publish request is 256KB, except for SMS messages where the limit is 1600 characters/runes.
	messageSizeLimit := 256 * 1024
	if n.conf.TopicARN != "" {
		topicTmpl := tmpl(n.conf.TopicARN)
		publishInput.SetTopicArn(topicTmpl)
		if n.isFifo == nil {
			// If we are using a topic ARN it could be a FIFO topic specified by the topic postfix .fifo.
			n.isFifo = aws.Bool(n.conf.TopicARN[len(n.conf.TopicARN)-5:] == ".fifo")
		}
		if *n.isFifo {
			// Deduplication key and Message Group ID are only added if it's a FIFO SNS Topic.
			key, err := notify.ExtractGroupKey(ctx)
			if err != nil {
				return nil, err
			}
			publishInput.SetMessageDeduplicationId(key.Hash())
			publishInput.SetMessageGroupId(key.Hash())
		}
	}
	if n.conf.PhoneNumber != "" {
		publishInput.SetPhoneNumber(tmpl(n.conf.PhoneNumber))
		// If we have an SMS message, we need to truncate to 1600 characters/runes.
		messageSizeLimit = 1600
	}
	if n.conf.TargetARN != "" {
		publishInput.SetTargetArn(tmpl(n.conf.TargetARN))
	}

	messageToSend, isTrunc, err := validateAndTruncateMessage(tmpl(n.conf.Message), messageSizeLimit)
	if err != nil {
		return nil, err
	}
	if isTrunc {
		// If we truncated the message we need to add a message attribute showing that it was truncated.
		messageAttributes["truncated"] = &sns.MessageAttributeValue{DataType: aws.String("String"), StringValue: aws.String("true")}
	}

	publishInput.SetMessage(messageToSend)
	publishInput.SetMessageAttributes(messageAttributes)

	if n.conf.Subject != "" {
		publishInput.SetSubject(tmpl(n.conf.Subject))
	}

	return publishInput, nil
}

func validateAndTruncateMessage(message string, maxMessageSizeInBytes int) (string, bool, error) {
	if !utf8.ValidString(message) {
		return "", false, fmt.Errorf("non utf8 encoded message string")
	}
	if len(message) <= maxMessageSizeInBytes {
		return message, false, nil
	}
	// if the message is larger than our specified size we have to truncate.
	truncated := make([]byte, maxMessageSizeInBytes)
	copy(truncated, message)
	return string(truncated), true, nil
}

func createMessageAttributes(n *Notifier, tmpl func(string) string) map[string]*sns.MessageAttributeValue {
	// Convert the given attributes map into the AWS Message Attributes Format
	attributes := make(map[string]*sns.MessageAttributeValue, len(n.conf.Attributes))
	for k, v := range n.conf.Attributes {
		attributes[tmpl(k)] = &sns.MessageAttributeValue{DataType: aws.String("String"), StringValue: aws.String(tmpl(v))}
	}
	return attributes
}

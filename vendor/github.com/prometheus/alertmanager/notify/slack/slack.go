// Copyright 2019 Prometheus Team
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

package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"github.com/pkg/errors"

	"github.com/go-kit/kit/log"
	commoncfg "github.com/prometheus/common/config"

	"github.com/prometheus/alertmanager/config"
	"github.com/prometheus/alertmanager/notify"
	"github.com/prometheus/alertmanager/template"
	"github.com/prometheus/alertmanager/types"
)

// Notifier implements a Notifier for Slack notifications.
type Notifier struct {
	conf    *config.SlackConfig
	tmpl    *template.Template
	logger  log.Logger
	client  *http.Client
	retrier *notify.Retrier
}

// New returns a new Slack notification handler.
func New(c *config.SlackConfig, t *template.Template, l log.Logger, httpOpts ...commoncfg.HTTPClientOption) (*Notifier, error) {
	client, err := commoncfg.NewClientFromConfig(*c.HTTPConfig, "slack", append(httpOpts, commoncfg.WithHTTP2Disabled())...)
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

// request is the request for sending a slack notification.
type request struct {
	Channel     string       `json:"channel,omitempty"`
	Username    string       `json:"username,omitempty"`
	IconEmoji   string       `json:"icon_emoji,omitempty"`
	IconURL     string       `json:"icon_url,omitempty"`
	LinkNames   bool         `json:"link_names,omitempty"`
	Attachments []attachment `json:"attachments"`
}

// attachment is used to display a richly-formatted message block.
type attachment struct {
	Title      string               `json:"title,omitempty"`
	TitleLink  string               `json:"title_link,omitempty"`
	Pretext    string               `json:"pretext,omitempty"`
	Text       string               `json:"text"`
	Fallback   string               `json:"fallback"`
	CallbackID string               `json:"callback_id"`
	Fields     []config.SlackField  `json:"fields,omitempty"`
	Actions    []config.SlackAction `json:"actions,omitempty"`
	ImageURL   string               `json:"image_url,omitempty"`
	ThumbURL   string               `json:"thumb_url,omitempty"`
	Footer     string               `json:"footer"`
	Color      string               `json:"color,omitempty"`
	MrkdwnIn   []string             `json:"mrkdwn_in,omitempty"`
}

// Notify implements the Notifier interface.
func (n *Notifier) Notify(ctx context.Context, as ...*types.Alert) (bool, error) {
	var err error
	var (
		data     = notify.GetTemplateData(ctx, n.tmpl, as, n.logger)
		tmplText = notify.TmplText(n.tmpl, data, &err)
	)
	var markdownIn []string
	if len(n.conf.MrkdwnIn) == 0 {
		markdownIn = []string{"fallback", "pretext", "text"}
	} else {
		markdownIn = n.conf.MrkdwnIn
	}
	att := &attachment{
		Title:      tmplText(n.conf.Title),
		TitleLink:  tmplText(n.conf.TitleLink),
		Pretext:    tmplText(n.conf.Pretext),
		Text:       tmplText(n.conf.Text),
		Fallback:   tmplText(n.conf.Fallback),
		CallbackID: tmplText(n.conf.CallbackID),
		ImageURL:   tmplText(n.conf.ImageURL),
		ThumbURL:   tmplText(n.conf.ThumbURL),
		Footer:     tmplText(n.conf.Footer),
		Color:      tmplText(n.conf.Color),
		MrkdwnIn:   markdownIn,
	}

	var numFields = len(n.conf.Fields)
	if numFields > 0 {
		var fields = make([]config.SlackField, numFields)
		for index, field := range n.conf.Fields {
			// Check if short was defined for the field otherwise fallback to the global setting
			var short bool
			if field.Short != nil {
				short = *field.Short
			} else {
				short = n.conf.ShortFields
			}

			// Rebuild the field by executing any templates and setting the new value for short
			fields[index] = config.SlackField{
				Title: tmplText(field.Title),
				Value: tmplText(field.Value),
				Short: &short,
			}
		}
		att.Fields = fields
	}

	var numActions = len(n.conf.Actions)
	if numActions > 0 {
		var actions = make([]config.SlackAction, numActions)
		for index, action := range n.conf.Actions {
			slackAction := config.SlackAction{
				Type:  tmplText(action.Type),
				Text:  tmplText(action.Text),
				URL:   tmplText(action.URL),
				Style: tmplText(action.Style),
				Name:  tmplText(action.Name),
				Value: tmplText(action.Value),
			}

			if action.ConfirmField != nil {
				slackAction.ConfirmField = &config.SlackConfirmationField{
					Title:       tmplText(action.ConfirmField.Title),
					Text:        tmplText(action.ConfirmField.Text),
					OkText:      tmplText(action.ConfirmField.OkText),
					DismissText: tmplText(action.ConfirmField.DismissText),
				}
			}

			actions[index] = slackAction
		}
		att.Actions = actions
	}

	req := &request{
		Channel:     tmplText(n.conf.Channel),
		Username:    tmplText(n.conf.Username),
		IconEmoji:   tmplText(n.conf.IconEmoji),
		IconURL:     tmplText(n.conf.IconURL),
		LinkNames:   n.conf.LinkNames,
		Attachments: []attachment{*att},
	}
	if err != nil {
		return false, err
	}

	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(req); err != nil {
		return false, err
	}

	var u string
	if n.conf.APIURL != nil {
		u = n.conf.APIURL.String()
	} else {
		content, err := ioutil.ReadFile(n.conf.APIURLFile)
		if err != nil {
			return false, err
		}
		u = string(content)
	}

	resp, err := notify.PostJSON(ctx, n.client, u, &buf)
	if err != nil {
		return true, notify.RedactURL(err)
	}
	defer notify.Drain(resp)

	// Only 5xx response codes are recoverable and 2xx codes are successful.
	// https://api.slack.com/incoming-webhooks#handling_errors
	// https://api.slack.com/changelog/2016-05-17-changes-to-errors-for-incoming-webhooks
	retry, err := n.retrier.Check(resp.StatusCode, resp.Body)
	err = errors.Wrap(err, fmt.Sprintf("channel %q", req.Channel))
	return retry, err
}

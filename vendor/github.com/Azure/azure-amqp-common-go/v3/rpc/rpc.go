// Package rpc provides functionality for request / reply messaging. It is used by package mgmt and cbs.
package rpc

//	MIT License
//
//	Copyright (c) Microsoft Corporation. All rights reserved.
//
//	Permission is hereby granted, free of charge, to any person obtaining a copy
//	of this software and associated documentation files (the "Software"), to deal
//	in the Software without restriction, including without limitation the rights
//	to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
//	copies of the Software, and to permit persons to whom the Software is
//	furnished to do so, subject to the following conditions:
//
//	The above copyright notice and this permission notice shall be included in all
//	copies or substantial portions of the Software.
//
//	THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
//	IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
//	FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
//	AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
//	LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
//	OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
//	SOFTWARE

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/devigned/tab"

	"github.com/Azure/azure-amqp-common-go/v3"
	"github.com/Azure/azure-amqp-common-go/v3/internal/tracing"
	"github.com/Azure/azure-amqp-common-go/v3/uuid"
	"github.com/Azure/go-amqp"
)

const (
	replyPostfix   = "-reply-to-"
	statusCodeKey  = "status-code"
	descriptionKey = "status-description"
)

type (
	// Link is the bidirectional communication structure used for CBS negotiation
	Link struct {
		session       *amqp.Session
		receiver      *amqp.Receiver
		sender        *amqp.Sender
		clientAddress string
		rpcMu         sync.Mutex
		sessionID     *string
		useSessionID  bool
		id            string
	}

	// Response is the simplified response structure from an RPC like call
	Response struct {
		Code        int
		Description string
		Message     *amqp.Message
	}

	// LinkOption provides a way to customize the construction of a Link
	LinkOption func(link *Link) error
)

// LinkWithSessionFilter configures a Link to use a session filter
func LinkWithSessionFilter(sessionID *string) LinkOption {
	return func(l *Link) error {
		l.sessionID = sessionID
		l.useSessionID = true
		return nil
	}
}

// NewLink will build a new request response link
func NewLink(conn *amqp.Client, address string, opts ...LinkOption) (*Link, error) {
	authSession, err := conn.NewSession()
	if err != nil {
		return nil, err
	}

	return NewLinkWithSession(authSession, address, opts...)
}

// NewLinkWithSession will build a new request response link, but will reuse an existing AMQP session
func NewLinkWithSession(session *amqp.Session, address string, opts ...LinkOption) (*Link, error) {
	linkID, err := uuid.NewV4()
	if err != nil {
		return nil, err
	}

	id := linkID.String()
	link := &Link{
		session:       session,
		clientAddress: strings.Replace("$", "", address, -1) + replyPostfix + id,
		id:            id,
	}

	for _, opt := range opts {
		if err := opt(link); err != nil {
			return nil, err
		}
	}

	sender, err := session.NewSender(
		amqp.LinkTargetAddress(address),
	)
	if err != nil {
		return nil, err
	}

	receiverOpts := []amqp.LinkOption{
		amqp.LinkSourceAddress(address),
		amqp.LinkTargetAddress(link.clientAddress),
	}

	if link.sessionID != nil {
		const name = "com.microsoft:session-filter"
		const code = uint64(0x00000137000000C)
		if link.sessionID == nil {
			receiverOpts = append(receiverOpts, amqp.LinkSourceFilter(name, code, nil))
		} else {
			receiverOpts = append(receiverOpts, amqp.LinkSourceFilter(name, code, link.sessionID))
		}
		receiverOpts = append(receiverOpts)
	}

	receiver, err := session.NewReceiver(receiverOpts...)
	if err != nil {
		// make sure we close the sender
		clsCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_ = sender.Close(clsCtx)
		return nil, err
	}

	link.sender = sender
	link.receiver = receiver

	return link, nil
}

// RetryableRPC attempts to retry a request a number of times with delay
func (l *Link) RetryableRPC(ctx context.Context, times int, delay time.Duration, msg *amqp.Message) (*Response, error) {
	ctx, span := tracing.StartSpanFromContext(ctx, "az-amqp-common.rpc.RetryableRPC")
	defer span.End()

	res, err := common.Retry(times, delay, func() (interface{}, error) {
		ctx, span := tracing.StartSpanFromContext(ctx, "az-amqp-common.rpc.RetryableRPC.retry")
		defer span.End()

		res, err := l.RPC(ctx, msg)
		if err != nil {
			tab.For(ctx).Error(fmt.Errorf("error in RPC via link %s: %v", l.id, err))
			return nil, err
		}

		switch {
		case res.Code >= 200 && res.Code < 300:
			tab.For(ctx).Debug(fmt.Sprintf("successful rpc on link %s: status code %d and description: %s", l.id, res.Code, res.Description))
			return res, nil
		case res.Code >= 500:
			errMessage := fmt.Sprintf("server error link %s: status code %d and description: %s", l.id, res.Code, res.Description)
			tab.For(ctx).Error(errors.New(errMessage))
			return nil, common.Retryable(errMessage)
		default:
			errMessage := fmt.Sprintf("unhandled error link %s: status code %d and description: %s", l.id, res.Code, res.Description)
			tab.For(ctx).Error(errors.New(errMessage))
			return nil, common.Retryable(errMessage)
		}
	})
	if err != nil {
		tab.For(ctx).Error(err)
		return nil, err
	}
	return res.(*Response), nil
}

// RPC sends a request and waits on a response for that request
func (l *Link) RPC(ctx context.Context, msg *amqp.Message) (*Response, error) {
	const altStatusCodeKey, altDescriptionKey = "statusCode", "statusDescription"

	l.rpcMu.Lock()
	defer l.rpcMu.Unlock()

	ctx, span := tracing.StartSpanFromContext(ctx, "az-amqp-common.rpc.RPC")
	defer span.End()

	if msg.Properties == nil {
		msg.Properties = &amqp.MessageProperties{}
	}
	msg.Properties.ReplyTo = l.clientAddress

	if msg.ApplicationProperties == nil {
		msg.ApplicationProperties = make(map[string]interface{})
	}

	if _, ok := msg.ApplicationProperties["server-timeout"]; !ok {
		if deadline, ok := ctx.Deadline(); ok {
			msg.ApplicationProperties["server-timeout"] = uint(time.Until(deadline) / time.Millisecond)
		}
	}

	err := l.sender.Send(ctx, msg)
	if err != nil {
		tab.For(ctx).Error(err)
		return nil, err
	}

	res, err := l.receiver.Receive(ctx)
	if err != nil {
		tab.For(ctx).Error(err)
		return nil, err
	}

	var statusCode int
	statusCodeCandidates := []string{statusCodeKey, altStatusCodeKey}
	for i := range statusCodeCandidates {
		if rawStatusCode, ok := res.ApplicationProperties[statusCodeCandidates[i]]; ok {
			if cast, ok := rawStatusCode.(int32); ok {
				statusCode = int(cast)
				break
			} else {
				err := errors.New("status code was not of expected type int32")
				tab.For(ctx).Error(err)
				return nil, err
			}
		}
	}
	if statusCode == 0 {
		err := errors.New("status codes was not found on rpc message")
		tab.For(ctx).Error(err)
		return nil, err
	}

	var description string
	descriptionCandidates := []string{descriptionKey, altDescriptionKey}
	for i := range descriptionCandidates {
		if rawDescription, ok := res.ApplicationProperties[descriptionCandidates[i]]; ok {
			if description, ok = rawDescription.(string); ok || rawDescription == nil {
				break
			} else {
				return nil, errors.New("status description was not of expected type string")
			}
		}
	}

	span.AddAttributes(tab.StringAttribute("http.status_code", fmt.Sprintf("%d", statusCode)))

	response := &Response{
		Code:        int(statusCode),
		Description: description,
		Message:     res,
	}

	if err := res.Accept(); err != nil {
		tab.For(ctx).Error(err)
		return response, err
	}

	return response, err
}

// Close the link receiver, sender and session
func (l *Link) Close(ctx context.Context) error {
	ctx, span := tracing.StartSpanFromContext(ctx, "az-amqp-common.rpc.Close")
	defer span.End()

	if err := l.closeReceiver(ctx); err != nil {
		_ = l.closeSender(ctx)
		_ = l.closeSession(ctx)
		return err
	}

	if err := l.closeSender(ctx); err != nil {
		_ = l.closeSession(ctx)
		return err
	}

	return l.closeSession(ctx)
}

func (l *Link) closeReceiver(ctx context.Context) error {
	ctx, span := tracing.StartSpanFromContext(ctx, "az-amqp-common.rpc.closeReceiver")
	defer span.End()

	if l.receiver != nil {
		return l.receiver.Close(ctx)
	}
	return nil
}

func (l *Link) closeSender(ctx context.Context) error {
	ctx, span := tracing.StartSpanFromContext(ctx, "az-amqp-common.rpc.closeSender")
	defer span.End()

	if l.sender != nil {
		return l.sender.Close(ctx)
	}
	return nil
}

func (l *Link) closeSession(ctx context.Context) error {
	ctx, span := tracing.StartSpanFromContext(ctx, "az-amqp-common.rpc.closeSession")
	defer span.End()

	if l.session != nil {
		return l.session.Close(ctx)
	}
	return nil
}

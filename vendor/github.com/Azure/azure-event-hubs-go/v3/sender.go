package eventhub

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
	"fmt"
	"net"
	"time"

	"github.com/Azure/azure-amqp-common-go/v3/uuid"
	"github.com/devigned/tab"
	"github.com/jpillora/backoff"
	"github.com/Azure/go-amqp"
)

// sender provides session and link handling for an sending entity path
type (
	sender struct {
		hub             *Hub
		connection      *amqp.Client
		session         *session
		sender          *amqp.Sender
		partitionID     *string
		Name            string
		recoveryBackoff *backoff.Backoff
	}

	// SendOption provides a way to customize a message on sending
	SendOption func(event *Event) error

	eventer interface {
		tab.Carrier
		toMsg() (*amqp.Message, error)
	}
)

// newSender creates a new Service Bus message sender given an AMQP client and entity path
func (h *Hub) newSender(ctx context.Context) (*sender, error) {
	span, ctx := h.startSpanFromContext(ctx, "eh.sender.newSender")
	defer span.End()

	s := &sender{
		hub:         h,
		partitionID: h.senderPartitionID,
		recoveryBackoff: &backoff.Backoff{
			Min:    10 * time.Millisecond,
			Max:    4 * time.Second,
			Jitter: true,
		},
	}
	tab.For(ctx).Debug(fmt.Sprintf("creating a new sender for entity path %s", s.getAddress()))
	err := s.newSessionAndLink(ctx)
	return s, err
}

// Recover will attempt to close the current session and link, then rebuild them
func (s *sender) Recover(ctx context.Context) error {
	span, ctx := s.startProducerSpanFromContext(ctx, "eh.sender.Recover")
	defer span.End()

	// we expect the sender, session or client is in an error state, ignore errors
	closeCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = s.sender.Close(closeCtx)
	_ = s.session.Close(closeCtx)
	_ = s.connection.Close()
	return s.newSessionAndLink(ctx)
}

// Close will close the AMQP connection, session and link of the sender
func (s *sender) Close(ctx context.Context) error {
	span, _ := s.startProducerSpanFromContext(ctx, "eh.sender.Close")
	defer span.End()

	err := s.sender.Close(ctx)
	if err != nil {
		tab.For(ctx).Error(err)
		if sessionErr := s.session.Close(ctx); sessionErr != nil {
			tab.For(ctx).Error(sessionErr)
		}

		if connErr := s.connection.Close(); connErr != nil {
			tab.For(ctx).Error(connErr)
		}

		return err
	}

	if sessionErr := s.session.Close(ctx); sessionErr != nil {
		tab.For(ctx).Error(sessionErr)

		if connErr := s.connection.Close(); connErr != nil {
			tab.For(ctx).Error(connErr)
		}

		return sessionErr
	}

	return s.connection.Close()
}

// Send will send a message to the entity path with options
//
// This will retry sending the message if the server responds with a busy error.
func (s *sender) Send(ctx context.Context, event *Event, opts ...SendOption) error {
	span, ctx := s.startProducerSpanFromContext(ctx, "eh.sender.Send")
	defer span.End()

	for _, opt := range opts {
		err := opt(event)
		if err != nil {
			return err
		}
	}

	if event.ID == "" {
		id, err := uuid.NewV4()
		if err != nil {
			return err
		}
		event.ID = id.String()
	}

	return s.trySend(ctx, event)
}

func (s *sender) trySend(ctx context.Context, evt eventer) error {
	sp, ctx := s.startProducerSpanFromContext(ctx, "eh.sender.trySend")
	defer sp.End()

	if err := sp.Inject(evt); err != nil {
		tab.For(ctx).Error(err)
		return err
	}

	msg, err := evt.toMsg()
	if err != nil {
		tab.For(ctx).Error(err)
		return err
	}

	if str, ok := msg.Properties.MessageID.(string); ok {
		sp.AddAttributes(tab.StringAttribute("he.message_id", str))
	}

	recvr := func(err error) {
		duration := s.recoveryBackoff.Duration()
		tab.For(ctx).Debug("amqp error, delaying " + string(duration/time.Millisecond) + " millis: " + err.Error())
		time.Sleep(duration)
		err = s.Recover(ctx)
		if err != nil {
			tab.For(ctx).Debug("failed to recover connection")
		} else {
			tab.For(ctx).Debug("recovered connection")
			s.recoveryBackoff.Reset()
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// try as long as the context is not dead
			err := s.sender.Send(ctx, msg)
			if err == nil {
				// successful send
				return err
			}
			switch err.(type) {
			case *amqp.Error, *amqp.DetachError, net.Error:
				if netErr, ok := err.(net.Error); ok {
					if !netErr.Temporary() {
						return netErr
					}
				}

				recvr(err)
			default:
				if !isRecoverableCloseError(err) {
					return err
				}

				recvr(err)
			}
		}
	}
}

func (s *sender) String() string {
	return s.Name
}

func (s *sender) getAddress() string {
	if s.partitionID != nil {
		return fmt.Sprintf("%s/Partitions/%s", s.hub.name, *s.partitionID)
	}
	return s.hub.name
}

func (s *sender) getFullIdentifier() string {
	return s.hub.namespace.getEntityAudience(s.getAddress())
}

// newSessionAndLink will replace the existing session and link
func (s *sender) newSessionAndLink(ctx context.Context) error {
	span, ctx := s.startProducerSpanFromContext(ctx, "eh.sender.newSessionAndLink")
	defer span.End()

	connection, err := s.hub.namespace.newConnection()
	if err != nil {
		tab.For(ctx).Error(err)
		return err
	}
	s.connection = connection

	err = s.hub.namespace.negotiateClaim(ctx, connection, s.getAddress())
	if err != nil {
		tab.For(ctx).Error(err)
		return err
	}

	amqpSession, err := connection.NewSession()
	if err != nil {
		tab.For(ctx).Error(err)
		return err
	}

	amqpSender, err := amqpSession.NewSender(
		amqp.LinkSenderSettle(amqp.ModeMixed),
		amqp.LinkReceiverSettle(amqp.ModeFirst),
		amqp.LinkTargetAddress(s.getAddress()),
	)
	if err != nil {
		tab.For(ctx).Error(err)
		return err
	}

	s.session, err = newSession(amqpSession)
	if err != nil {
		tab.For(ctx).Error(err)
		return err
	}

	s.sender = amqpSender
	return nil
}

// SendWithMessageID configures the message with a message ID
func SendWithMessageID(messageID string) SendOption {
	return func(event *Event) error {
		event.ID = messageID
		return nil
	}
}

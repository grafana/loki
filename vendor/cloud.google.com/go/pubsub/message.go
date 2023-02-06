// Copyright 2016 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pubsub

import (
	"fmt"
	"time"

	ipubsub "cloud.google.com/go/internal/pubsub"
	pb "cloud.google.com/go/pubsub/apiv1/pubsubpb"
)

// Message represents a Pub/Sub message.
//
// Message can be passed to Topic.Publish for publishing.
//
// If received in the callback passed to Subscription.Receive, client code must
// call Message.Ack or Message.Nack when finished processing the Message. Calls
// to Ack or Nack have no effect after the first call.
//
// Ack indicates successful processing of a Message. If message acknowledgement
// fails, the Message will be redelivered. Nack indicates that the client will
// not or cannot process a Message. Nack will result in the Message being
// redelivered more quickly than if it were allowed to expire.
//
// If using exactly once delivery, you should call Message.AckWithResult and
// Message.NackWithResult instead. These methods will return an AckResult,
// which tracks the state of acknowledgement operation. If the AckResult returns
// successful, the message is guaranteed NOT to be re-delivered. Otherwise,
// the AckResult will return an error with more details about the failure
// and the message may be re-delivered.
type Message = ipubsub.Message

// msgAckHandler performs a safe cast of the message's ack handler to psAckHandler.
func msgAckHandler(m *Message, eod bool) (*psAckHandler, bool) {
	ackh, ok := ipubsub.MessageAckHandler(m).(*psAckHandler)
	ackh.exactlyOnceDelivery = eod
	return ackh, ok
}

func msgAckID(m *Message) string {
	if ackh, ok := msgAckHandler(m, false); ok {
		return ackh.ackID
	}
	return ""
}

// The done method of the iterator that created a Message.
type iterDoneFunc func(string, bool, *AckResult, time.Time)

func convertMessages(rms []*pb.ReceivedMessage, receiveTime time.Time, doneFunc iterDoneFunc) ([]*Message, error) {
	msgs := make([]*Message, 0, len(rms))
	for i, m := range rms {
		msg, err := toMessage(m, receiveTime, doneFunc)
		if err != nil {
			return nil, fmt.Errorf("pubsub: cannot decode the retrieved message at index: %d, message: %+v", i, m)
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}

func toMessage(resp *pb.ReceivedMessage, receiveTime time.Time, doneFunc iterDoneFunc) (*Message, error) {
	ackh := &psAckHandler{ackID: resp.AckId}
	msg := ipubsub.NewMessage(ackh)
	if resp.Message == nil {
		return msg, nil
	}

	pubTime := resp.Message.PublishTime.AsTime()

	var deliveryAttempt *int
	if resp.DeliveryAttempt > 0 {
		da := int(resp.DeliveryAttempt)
		deliveryAttempt = &da
	}

	msg.Data = resp.Message.Data
	msg.Attributes = resp.Message.Attributes
	msg.ID = resp.Message.MessageId
	msg.PublishTime = pubTime
	msg.DeliveryAttempt = deliveryAttempt
	msg.OrderingKey = resp.Message.OrderingKey
	ackh.receiveTime = receiveTime
	ackh.doneFunc = doneFunc
	ackh.ackResult = ipubsub.NewAckResult()
	return msg, nil
}

// AckResult holds the result from a call to Ack or Nack.
//
// Call Get to obtain the result of the Ack/NackWithResult call. Example:
//
//	// Get blocks until Ack/NackWithResult completes or ctx is done.
//	ackStatus, err := r.Get(ctx)
//	if err != nil {
//	    // TODO: Handle error.
//	}
type AckResult = ipubsub.AckResult

// AcknowledgeStatus represents the status of an Ack or Nack request.
type AcknowledgeStatus = ipubsub.AcknowledgeStatus

const (
	// AcknowledgeStatusSuccess indicates the request was a success.
	AcknowledgeStatusSuccess AcknowledgeStatus = iota
	// AcknowledgeStatusPermissionDenied indicates the caller does not have sufficient permissions.
	AcknowledgeStatusPermissionDenied
	// AcknowledgeStatusFailedPrecondition indicates the request encountered a FailedPrecondition error.
	AcknowledgeStatusFailedPrecondition
	// AcknowledgeStatusInvalidAckID indicates one or more of the ack IDs sent were invalid.
	AcknowledgeStatusInvalidAckID
	// AcknowledgeStatusOther indicates another unknown error was returned.
	AcknowledgeStatusOther
)

// psAckHandler handles ack/nack for the pubsub package.
type psAckHandler struct {
	// ackID is the identifier to acknowledge this message.
	ackID string

	// receiveTime is the time the message was received by the client.
	receiveTime time.Time

	calledDone bool

	// The done method of the iterator that created this Message.
	doneFunc iterDoneFunc

	// the ack result that will be returned for this ack handler
	// if AckWithResult or NackWithResult is called.
	ackResult *AckResult

	// exactlyOnceDelivery determines if the message needs to be delivered
	// exactly once.
	exactlyOnceDelivery bool
}

func (ah *psAckHandler) OnAck() {
	ah.done(true)
}

func (ah *psAckHandler) OnNack() {
	ah.done(false)
}

func (ah *psAckHandler) OnAckWithResult() *AckResult {
	if !ah.exactlyOnceDelivery {
		return newSuccessAckResult()
	}
	// call done with true to indicate ack.
	ah.done(true)
	return ah.ackResult
}

func (ah *psAckHandler) OnNackWithResult() *AckResult {
	if !ah.exactlyOnceDelivery {
		return newSuccessAckResult()
	}
	// call done with false to indicate nack.
	ah.done(false)
	return ah.ackResult
}

func (ah *psAckHandler) done(ack bool) {
	if ah.calledDone {
		return
	}
	ah.calledDone = true
	if ah.doneFunc != nil {
		ah.doneFunc(ah.ackID, ack, ah.ackResult, ah.receiveTime)
	}
}

// newSuccessAckResult returns an AckResult that resolves to success immediately.
func newSuccessAckResult() *AckResult {
	ar := ipubsub.NewAckResult()
	ipubsub.SetAckResult(ar, AcknowledgeStatusSuccess, nil)
	return ar
}

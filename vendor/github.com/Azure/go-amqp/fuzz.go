// +build gofuzz

package amqp

import (
	"context"
	"time"

	"github.com/Azure/go-amqp/internal/testconn"
)

func FuzzConn(data []byte) int {
	// Receive
	client, err := New(testconn.New(data),
		ConnSASLPlain("listen", "3aCXZYFcuZA89xe6lZkfYJvOPnTGipA3ap7NvPruBhI="),
		ConnIdleTimeout(10*time.Millisecond),
	)
	if err != nil {
		return 0
	}
	defer client.Close()

	s, err := client.NewSession()
	if err != nil {
		return 0
	}

	r, err := s.NewReceiver(LinkSourceAddress("source"), LinkCredit(2))
	if err != nil {
		return 0
	}

	msg, err := r.Receive(context.Background())
	if err != nil {
		return 0
	}

	msg.Accept()

	ctx, close := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer close()

	r.Close(ctx)

	s.Close(ctx)

	// Send
	client, err = New(testconn.New(data),
		ConnSASLPlain("listen", "3aCXZYFcuZA89xe6lZkfYJvOPnTGipA3ap7NvPruBhI="),
		ConnIdleTimeout(10*time.Millisecond),
	)
	if err != nil {
		return 0
	}
	defer client.Close()

	s, err = client.NewSession()
	if err != nil {
		return 0
	}

	sender, err := s.NewSender(LinkTargetAddress("source"), LinkCredit(2))
	if err != nil {
		return 0
	}

	err = sender.Send(context.Background(), NewMessage(data))
	if err != nil {
		return 0
	}

	ctx, close = context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer close()

	r.Close(ctx)

	s.Close(ctx)

	return 1
}

func FuzzUnmarshal(data []byte) int {
	types := []interface{}{
		new(performAttach),
		new(*performAttach),
		new(performBegin),
		new(*performBegin),
		new(performClose),
		new(*performClose),
		new(performDetach),
		new(*performDetach),
		new(performDisposition),
		new(*performDisposition),
		new(performEnd),
		new(*performEnd),
		new(performFlow),
		new(*performFlow),
		new(performOpen),
		new(*performOpen),
		new(performTransfer),
		new(*performTransfer),
		new(source),
		new(*source),
		new(target),
		new(*target),
		new(Error),
		new(*Error),
		new(saslCode),
		new(*saslCode),
		new(saslMechanisms),
		new(*saslMechanisms),
		new(saslChallenge),
		new(*saslChallenge),
		new(saslResponse),
		new(*saslResponse),
		new(saslOutcome),
		new(*saslOutcome),
		new(Message),
		new(*Message),
		new(MessageHeader),
		new(*MessageHeader),
		new(MessageProperties),
		new(*MessageProperties),
		new(stateReceived),
		new(*stateReceived),
		new(stateAccepted),
		new(*stateAccepted),
		new(stateRejected),
		new(*stateRejected),
		new(stateReleased),
		new(*stateReleased),
		new(stateModified),
		new(*stateModified),
		new(mapAnyAny),
		new(*mapAnyAny),
		new(mapStringAny),
		new(*mapStringAny),
		new(mapSymbolAny),
		new(*mapSymbolAny),
		new(unsettled),
		new(*unsettled),
		new(milliseconds),
		new(*milliseconds),
		new(bool),
		new(*bool),
		new(int8),
		new(*int8),
		new(int16),
		new(*int16),
		new(int32),
		new(*int32),
		new(int64),
		new(*int64),
		new(uint8),
		new(*uint8),
		new(uint16),
		new(*uint16),
		new(uint32),
		new(*uint32),
		new(uint64),
		new(*uint64),
		new(time.Time),
		new(*time.Time),
		new(time.Duration),
		new(*time.Duration),
		new(symbol),
		new(*symbol),
		new([]byte),
		new(*[]byte),
		new([]string),
		new(*[]string),
		new([]symbol),
		new(*[]symbol),
		new(map[interface{}]interface{}),
		new(*map[interface{}]interface{}),
		new(map[string]interface{}),
		new(*map[string]interface{}),
		new(map[symbol]interface{}),
		new(*map[symbol]interface{}),
		new(interface{}),
		new(*interface{}),
		new(ErrorCondition),
		new(*ErrorCondition),
		new(role),
		new(*role),
		new(UUID),
		new(*UUID),
	}

	for _, t := range types {
		unmarshal(&buffer{b: data}, t)
		readAny(&buffer{b: data})
	}
	return 0
}

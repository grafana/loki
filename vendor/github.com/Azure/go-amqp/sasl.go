package amqp

import (
	"fmt"
)

// SASL Codes
const (
	codeSASLOK      saslCode = iota // Connection authentication succeeded.
	codeSASLAuth                    // Connection authentication failed due to an unspecified problem with the supplied credentials.
	codeSASLSys                     // Connection authentication failed due to a system error.
	codeSASLSysPerm                 // Connection authentication failed due to a system error that is unlikely to be corrected without intervention.
	codeSASLSysTemp                 // Connection authentication failed due to a transient system error.
)

// SASL Mechanisms
const (
	saslMechanismPLAIN     symbol = "PLAIN"
	saslMechanismANONYMOUS symbol = "ANONYMOUS"
	saslMechanismXOAUTH2   symbol = "XOAUTH2"
)

type saslCode uint8

func (s saslCode) marshal(wr *buffer) error {
	return marshal(wr, uint8(s))
}

func (s *saslCode) unmarshal(r *buffer) error {
	n, err := readUbyte(r)
	*s = saslCode(n)
	return err
}

// ConnSASLPlain enables SASL PLAIN authentication for the connection.
//
// SASL PLAIN transmits credentials in plain text and should only be used
// on TLS/SSL enabled connection.
func ConnSASLPlain(username, password string) ConnOption {
	// TODO: how widely used is hostname? should it be supported
	return func(c *conn) error {
		// make handlers map if no other mechanism has
		if c.saslHandlers == nil {
			c.saslHandlers = make(map[symbol]stateFunc)
		}

		// add the handler the the map
		c.saslHandlers[saslMechanismPLAIN] = func() stateFunc {
			// send saslInit with PLAIN payload
			init := &saslInit{
				Mechanism:       "PLAIN",
				InitialResponse: []byte("\x00" + username + "\x00" + password),
				Hostname:        "",
			}
			debug(1, "TX: %s", init)
			c.err = c.writeFrame(frame{
				type_: frameTypeSASL,
				body:  init,
			})
			if c.err != nil {
				return nil
			}

			// go to c.saslOutcome to handle the server response
			return c.saslOutcome
		}
		return nil
	}
}

// ConnSASLAnonymous enables SASL ANONYMOUS authentication for the connection.
func ConnSASLAnonymous() ConnOption {
	return func(c *conn) error {
		// make handlers map if no other mechanism has
		if c.saslHandlers == nil {
			c.saslHandlers = make(map[symbol]stateFunc)
		}

		// add the handler the the map
		c.saslHandlers[saslMechanismANONYMOUS] = func() stateFunc {
			init := &saslInit{
				Mechanism:       saslMechanismANONYMOUS,
				InitialResponse: []byte("anonymous"),
			}
			debug(1, "TX: %s", init)
			c.err = c.writeFrame(frame{
				type_: frameTypeSASL,
				body:  init,
			})
			if c.err != nil {
				return nil
			}

			// go to c.saslOutcome to handle the server response
			return c.saslOutcome
		}
		return nil
	}
}

// ConnSASLXOAUTH2 enables SASL XOAUTH2 authentication for the connection.
//
// The saslMaxFrameSizeOverride parameter allows the limit that governs the maximum frame size this client will allow
// itself to generate to be raised for the sasl-init frame only.  Set this when the size of the size of the SASL XOAUTH2
// initial client response (which contains the username and bearer token) would otherwise breach the 512 byte min-max-frame-size
// (http://docs.oasis-open.org/amqp/core/v1.0/os/amqp-core-transport-v1.0-os.html#definition-MIN-MAX-FRAME-SIZE). Pass -1
// to keep the default.
//
// SASL XOAUTH2 transmits the bearer in plain text and should only be used
// on TLS/SSL enabled connection.
func ConnSASLXOAUTH2(username, bearer string, saslMaxFrameSizeOverride uint32) ConnOption {
	return func(c *conn) error {
		// make handlers map if no other mechanism has
		if c.saslHandlers == nil {
			c.saslHandlers = make(map[symbol]stateFunc)
		}

		response, err := saslXOAUTH2InitialResponse(username, bearer)
		if err != nil {
			return err
		}

		handler := saslXOAUTH2Handler{
			conn:                 c,
			maxFrameSizeOverride: saslMaxFrameSizeOverride,
			response:             response,
		}
		// add the handler the the map
		c.saslHandlers[saslMechanismXOAUTH2] = handler.init
		return nil
	}
}

type saslXOAUTH2Handler struct {
	conn                 *conn
	maxFrameSizeOverride uint32
	response             []byte
	errorResponse        []byte // https://developers.google.com/gmail/imap/xoauth2-protocol#error_response
}

func (s saslXOAUTH2Handler) init() stateFunc {
	originalPeerMaxFrameSize := s.conn.peerMaxFrameSize
	if s.maxFrameSizeOverride > s.conn.peerMaxFrameSize {
		s.conn.peerMaxFrameSize = s.maxFrameSizeOverride
	}
	s.conn.err = s.conn.writeFrame(frame{
		type_: frameTypeSASL,
		body: &saslInit{
			Mechanism:       saslMechanismXOAUTH2,
			InitialResponse: s.response,
		},
	})
	s.conn.peerMaxFrameSize = originalPeerMaxFrameSize
	if s.conn.err != nil {
		return nil
	}

	return s.step
}

func (s saslXOAUTH2Handler) step() stateFunc {
	// read challenge or outcome frame
	fr, err := s.conn.readFrame()
	if err != nil {
		s.conn.err = err
		return nil
	}

	switch v := fr.body.(type) {
	case *saslOutcome:
		// check if auth succeeded
		if v.Code != codeSASLOK {
			s.conn.err = errorErrorf("SASL XOAUTH2 auth failed with code %#00x: %s : %s",
				v.Code, v.AdditionalData, s.errorResponse)
			return nil
		}

		// return to c.negotiateProto
		s.conn.saslComplete = true
		return s.conn.negotiateProto
	case *saslChallenge:
		if s.errorResponse == nil {
			s.errorResponse = v.Challenge

			// The SASL protocol requires clients to send an empty response to this challenge.
			s.conn.err = s.conn.writeFrame(frame{
				type_: frameTypeSASL,
				body: &saslResponse{
					Response: []byte{},
				},
			})
			return s.step
		} else {
			s.conn.err = errorErrorf("SASL XOAUTH2 unexpected additional error response received during "+
				"exchange. Initial error response: %s, additional response: %s", s.errorResponse, v.Challenge)
			return nil
		}
	default:
		s.conn.err = errorErrorf("unexpected frame type %T", fr.body)
		return nil
	}
}

func saslXOAUTH2InitialResponse(username string, bearer string) ([]byte, error) {
	if len(bearer) == 0 {
		return []byte{}, fmt.Errorf("unacceptable bearer token")
	}
	for _, char := range bearer {
		if char < '\x20' || char > '\x7E' {
			return []byte{}, fmt.Errorf("unacceptable bearer token")
		}
	}
	for _, char := range username {
		if char == '\x01' {
			return []byte{}, fmt.Errorf("unacceptable username")
		}
	}
	return []byte("user=" + username + "\x01auth=Bearer " + bearer + "\x01\x01"), nil
}

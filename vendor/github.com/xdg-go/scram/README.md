[![Go Reference](https://pkg.go.dev/badge/github.com/xdg-go/scram.svg)](https://pkg.go.dev/github.com/xdg-go/scram)
[![Go Report Card](https://goreportcard.com/badge/github.com/xdg-go/scram)](https://goreportcard.com/report/github.com/xdg-go/scram)
[![Github Actions](https://github.com/xdg-go/scram/actions/workflows/test.yml/badge.svg)](https://github.com/xdg-go/scram/actions/workflows/test.yml)

# scram – Go implementation of RFC-5802

## Description

Package scram provides client and server implementations of the Salted
Challenge Response Authentication Mechanism (SCRAM) described in
- [RFC-5802](https://tools.ietf.org/html/rfc5802)
- [RFC-5929](https://tools.ietf.org/html/rfc5929)
- [RFC-7677](https://tools.ietf.org/html/rfc7677)
- [RFC-9266](https://tools.ietf.org/html/rfc9266)

It includes both client and server side support.

Channel binding is supported for SCRAM-PLUS variants, including:
- `tls-unique` (RFC 5929) - insecure, but required
- `tls-server-end-point` (RFC 5929) - works with all TLS versions
- `tls-exporter` (RFC 9266) - recommended for TLS 1.3+

SCRAM message extensions are not supported.

## Examples

### Client side

    package main

    import "github.com/xdg-go/scram"

    func main() {
        // Get Client with username, password and (optional) authorization ID.
        clientSHA1, err := scram.SHA1.NewClient("mulder", "trustno1", "")
        if err != nil {
            panic(err)
        }

        // Prepare the authentication conversation. Use the empty string as the
        // initial server message argument to start the conversation.
        conv := clientSHA1.NewConversation()
        var serverMsg string

        // Get the first message, send it and read the response.
        firstMsg, err := conv.Step(serverMsg)
        if err != nil {
            panic(err)
        }
        serverMsg = sendClientMsg(firstMsg)

        // Get the second message, send it, and read the response.
        secondMsg, err := conv.Step(serverMsg)
        if err != nil {
            panic(err)
        }
        serverMsg = sendClientMsg(secondMsg)

        // Validate the server's final message.  We have no further message to
        // send so ignore that return value.
        _, err = conv.Step(serverMsg)
        if err != nil {
            panic(err)
        }

        return
    }

    func sendClientMsg(s string) string {
        // A real implementation would send this to a server and read a reply.
        return ""
    }

### Client side with channel binding (SCRAM-PLUS)

    package main

    import (
        "crypto/tls"
        "github.com/xdg-go/scram"
    )

    func main() {
        // Establish TLS connection
        tlsConn, err := tls.Dial("tcp", "server:port", &tls.Config{MinVersion: tls.VersionTLS13})
        if err != nil {
            panic(err)
        }
        defer tlsConn.Close()

        // Get Client with username, password
        client, err := scram.SHA256.NewClient("mulder", "trustno1", "")
        if err != nil {
            panic(err)
        }

        // Create channel binding from TLS connection (TLS 1.3 example)
        // Use NewTLSExporterBinding for TLS 1.3+, NewTLSServerEndpointBinding for all TLS versions
        channelBinding, err := scram.NewTLSExporterBinding(&tlsConn.ConnectionState())
        if err != nil {
            panic(err)
        }

        // Create conversation with channel binding for SCRAM-SHA-256-PLUS
        conv := client.NewConversationWithChannelBinding(channelBinding)
        // ... rest of authentication conversation
    }

## Copyright and License

Copyright 2018 by David A. Golden. All rights reserved.

Licensed under the Apache License, Version 2.0 (the "License"). You may
obtain a copy of the License at http://www.apache.org/licenses/LICENSE-2.0

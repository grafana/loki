// Package cbs provides the functionality for negotiating claims-based security over AMQP for use in Azure Service Bus
// and Event Hubs.
package cbs

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
	"time"

	"github.com/devigned/tab"

	"github.com/Azure/azure-amqp-common-go/v3/auth"
	"github.com/Azure/azure-amqp-common-go/v3/internal/tracing"
	"github.com/Azure/azure-amqp-common-go/v3/rpc"
	"github.com/Azure/go-amqp"
)

const (
	cbsAddress           = "$cbs"
	cbsOperationKey      = "operation"
	cbsOperationPutToken = "put-token"
	cbsTokenTypeKey      = "type"
	cbsAudienceKey       = "name"
	cbsExpirationKey     = "expiration"
)

// NegotiateClaim attempts to put a token to the $cbs management endpoint to negotiate auth for the given audience
func NegotiateClaim(ctx context.Context, audience string, conn *amqp.Client, provider auth.TokenProvider) error {
	ctx, span := tracing.StartSpanFromContext(ctx, "az-amqp-common.cbs.NegotiateClaim")
	defer span.End()

	link, err := rpc.NewLink(conn, cbsAddress)
	if err != nil {
		tab.For(ctx).Error(err)
		return err
	}
	defer func() {
		if err := link.Close(ctx); err != nil {
			tab.For(ctx).Error(err)
		}
	}()

	token, err := provider.GetToken(audience)
	if err != nil {
		tab.For(ctx).Error(err)
		return err
	}

	tab.For(ctx).Debug(fmt.Sprintf("negotiating claim for audience %s with token type %s and expiry of %s", audience, token.TokenType, token.Expiry))
	msg := &amqp.Message{
		Value: token.Token,
		ApplicationProperties: map[string]interface{}{
			cbsOperationKey:  cbsOperationPutToken,
			cbsTokenTypeKey:  string(token.TokenType),
			cbsAudienceKey:   audience,
			cbsExpirationKey: token.Expiry,
		},
	}

	res, err := link.RetryableRPC(ctx, 3, 1*time.Second, msg)
	if err != nil {
		tab.For(ctx).Error(err)
		return err
	}

	tab.For(ctx).Debug(fmt.Sprintf("negotiated with response code %d and message: %s", res.Code, res.Description))
	return nil
}

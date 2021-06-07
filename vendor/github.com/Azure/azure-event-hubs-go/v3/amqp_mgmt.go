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
	"time"

	"github.com/Azure/azure-amqp-common-go/v3/rpc"
	"github.com/devigned/tab"
	"github.com/mitchellh/mapstructure"
	"github.com/Azure/go-amqp"
)

const (
	// MsftVendor is the Microsoft vendor identifier
	MsftVendor          = "com.microsoft"
	entityTypeKey       = "type"
	entityNameKey       = "name"
	partitionNameKey    = "partition"
	securityTokenKey    = "security_token"
	eventHubEntityType  = MsftVendor + ":eventhub"
	partitionEntityType = MsftVendor + ":partition"
	operationKey        = "operation"
	readOperationKey    = "READ"
	address             = "$management"
)

type (
	// client communicates with an AMQP management node
	client struct {
		namespace *namespace
		hubName   string
	}

	// HubRuntimeInformation provides management node information about a given Event Hub instance
	HubRuntimeInformation struct {
		Path           string    `mapstructure:"name"`
		CreatedAt      time.Time `mapstructure:"created_at"`
		PartitionCount int       `mapstructure:"partition_count"`
		PartitionIDs   []string  `mapstructure:"partition_ids"`
	}

	// HubPartitionRuntimeInformation provides management node information about a given Event Hub partition
	HubPartitionRuntimeInformation struct {
		HubPath                 string    `mapstructure:"name"`
		PartitionID             string    `mapstructure:"partition"`
		BeginningSequenceNumber int64     `mapstructure:"begin_sequence_number"`
		LastSequenceNumber      int64     `mapstructure:"last_enqueued_sequence_number"`
		LastEnqueuedOffset      string    `mapstructure:"last_enqueued_offset"`
		LastEnqueuedTimeUtc     time.Time `mapstructure:"last_enqueued_time_utc"`
	}
)

// newClient constructs a new AMQP management client
func newClient(namespace *namespace, hubName string) *client {
	return &client{
		namespace: namespace,
		hubName:   hubName,
	}
}

// GetHubRuntimeInformation requests runtime information for an Event Hub
func (c *client) GetHubRuntimeInformation(ctx context.Context, conn *amqp.Client) (*HubRuntimeInformation, error) {
	ctx, span := tab.StartSpan(ctx, "eh.mgmt.client.GetHubRuntimeInformation")
	defer span.End()

	rpcLink, err := rpc.NewLink(conn, address)
	if err != nil {
		return nil, err
	}

	msg := &amqp.Message{
		ApplicationProperties: map[string]interface{}{
			operationKey:  readOperationKey,
			entityTypeKey: eventHubEntityType,
			entityNameKey: c.hubName,
		},
	}
	msg, err = c.addSecurityToken(msg)
	if err != nil {
		return nil, err
	}

	res, err := rpcLink.RetryableRPC(ctx, 3, 1*time.Second, msg)
	if err != nil {
		return nil, err
	}

	hubRuntimeInfo, err := newHubRuntimeInformation(res.Message)
	if err != nil {
		return nil, err
	}
	return hubRuntimeInfo, nil
}

// GetHubPartitionRuntimeInformation fetches runtime information from the AMQP management node for a given partition
func (c *client) GetHubPartitionRuntimeInformation(ctx context.Context, conn *amqp.Client, partitionID string) (*HubPartitionRuntimeInformation, error) {
	ctx, span := tab.StartSpan(ctx, "eh.mgmt.client.GetHubPartitionRuntimeInformation")
	defer span.End()

	rpcLink, err := rpc.NewLink(conn, address)
	if err != nil {
		return nil, err
	}

	msg := &amqp.Message{
		ApplicationProperties: map[string]interface{}{
			operationKey:     readOperationKey,
			entityTypeKey:    partitionEntityType,
			entityNameKey:    c.hubName,
			partitionNameKey: partitionID,
		},
	}
	msg, err = c.addSecurityToken(msg)
	if err != nil {
		return nil, err
	}

	res, err := rpcLink.RetryableRPC(ctx, 3, 1*time.Second, msg)
	if err != nil {
		return nil, err
	}

	hubPartitionRuntimeInfo, err := newHubPartitionRuntimeInformation(res.Message)
	if err != nil {
		return nil, err
	}
	return hubPartitionRuntimeInfo, nil
}

func (c *client) addSecurityToken(msg *amqp.Message) (*amqp.Message, error) {
	token, err := c.namespace.tokenProvider.GetToken(c.getTokenAudience())
	if err != nil {
		return nil, err
	}
	msg.ApplicationProperties[securityTokenKey] = token.Token

	return msg, nil
}

func (c *client) getTokenAudience() string {
	return c.namespace.getAmqpHostURI() + c.hubName
}

func newHubPartitionRuntimeInformation(msg *amqp.Message) (*HubPartitionRuntimeInformation, error) {
	values, ok := msg.Value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("values were not map[string]interface{}, it was: %v", values)
	}

	var partitionInfo HubPartitionRuntimeInformation
	err := mapstructure.Decode(values, &partitionInfo)
	return &partitionInfo, err
}

// newHubRuntimeInformation constructs a new HubRuntimeInformation from an AMQP message
func newHubRuntimeInformation(msg *amqp.Message) (*HubRuntimeInformation, error) {
	values, ok := msg.Value.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("values were not map[string]interface{}, it was: %v", values)
	}

	var runtimeInfo HubRuntimeInformation
	err := mapstructure.Decode(values, &runtimeInfo)
	return &runtimeInfo, err
}

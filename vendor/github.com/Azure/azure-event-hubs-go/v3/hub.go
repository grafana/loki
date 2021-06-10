// Package eventhub provides functionality for interacting with Azure Event Hubs.
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
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"sync"

	"github.com/Azure/azure-amqp-common-go/v3/aad"
	"github.com/Azure/azure-amqp-common-go/v3/auth"
	"github.com/Azure/azure-amqp-common-go/v3/conn"
	"github.com/Azure/azure-amqp-common-go/v3/sas"
	"github.com/Azure/azure-amqp-common-go/v3/uuid"
	"github.com/Azure/azure-sdk-for-go/services/eventhub/mgmt/2017-04-01/eventhub"
	"github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/date"
	"github.com/Azure/go-autorest/autorest/to"
	"github.com/devigned/tab"
	"github.com/Azure/go-amqp"

	"github.com/Azure/azure-event-hubs-go/v3/atom"
	"github.com/Azure/azure-event-hubs-go/v3/persist"
)

const (
	maxUserAgentLen = 128
	rootUserAgent   = "/golang-event-hubs"
)

type (
	// Hub provides the ability to send and receive Event Hub messages
	Hub struct {
		name              string
		namespace         *namespace
		receivers         map[string]*receiver
		sender            *sender
		senderPartitionID *string
		receiverMu        sync.Mutex
		senderMu          sync.Mutex
		offsetPersister   persist.CheckpointPersister
		userAgent         string
	}

	// Handler is the function signature for any receiver of events
	Handler func(ctx context.Context, event *Event) error

	// Sender provides the ability to send a messages
	Sender interface {
		Send(ctx context.Context, event *Event, opts ...SendOption) error
		SendBatch(ctx context.Context, batch *EventBatch, opts ...SendOption) error
	}

	// PartitionedReceiver provides the ability to receive messages from a given partition
	PartitionedReceiver interface {
		Receive(ctx context.Context, partitionID string, handler Handler, opts ...ReceiveOption) (ListenerHandle, error)
	}

	// Manager provides the ability to query management node information about a node
	Manager interface {
		GetRuntimeInformation(context.Context) (HubRuntimeInformation, error)
		GetPartitionInformation(context.Context, string) (HubPartitionRuntimeInformation, error)
	}

	// HubOption provides structure for configuring new Event Hub clients. For building new Event Hubs, see
	// HubManagementOption.
	HubOption func(h *Hub) error

	// HubManager provides CRUD functionality for Event Hubs
	HubManager struct {
		*entityManager
	}

	// HubEntity is the Azure Event Hub description of a Hub for management activities
	HubEntity struct {
		*HubDescription
		Name string
	}

	// hubFeed is a specialized feed containing hubEntries
	hubFeed struct {
		*atom.Feed
		Entries []hubEntry `xml:"entry"`
	}

	// hubEntry is a specialized Hub feed entry
	hubEntry struct {
		*atom.Entry
		Content *hubContent `xml:"content"`
	}

	// hubContent is a specialized Hub body for an Atom entry
	hubContent struct {
		XMLName        xml.Name       `xml:"content"`
		Type           string         `xml:"type,attr"`
		HubDescription HubDescription `xml:"EventHubDescription"`
	}

	// HubDescription is the content type for Event Hub management requests
	HubDescription struct {
		XMLName                  xml.Name               `xml:"EventHubDescription"`
		MessageRetentionInDays   *int32                 `xml:"MessageRetentionInDays,omitempty"`
		SizeInBytes              *int64                 `xml:"SizeInBytes,omitempty"`
		Status                   *eventhub.EntityStatus `xml:"Status,omitempty"`
		CreatedAt                *date.Time             `xml:"CreatedAt,omitempty"`
		UpdatedAt                *date.Time             `xml:"UpdatedAt,omitempty"`
		PartitionCount           *int32                 `xml:"PartitionCount,omitempty"`
		PartitionIDs             *[]string              `xml:"PartitionIds>string,omitempty"`
		EntityAvailabilityStatus *string                `xml:"EntityAvailabilityStatus,omitempty"`
		BaseEntityDescription
	}

	// HubManagementOption provides structure for configuring new Event Hubs
	HubManagementOption func(description *HubDescription) error
)

// NewHubManagerFromConnectionString builds a HubManager from an Event Hub connection string
func NewHubManagerFromConnectionString(connStr string) (*HubManager, error) {
	ns, err := newNamespace(namespaceWithConnectionString(connStr))
	if err != nil {
		return nil, err
	}
	return &HubManager{
		entityManager: newEntityManager(ns.getHTTPSHostURI(), ns.tokenProvider),
	}, nil
}

// NewHubManagerFromAzureEnvironment builds a HubManager from a Event Hub name, SAS or AAD token provider and Azure Environment
func NewHubManagerFromAzureEnvironment(namespace string, tokenProvider auth.TokenProvider, env azure.Environment) (*HubManager, error) {
	ns, err := newNamespace(namespaceWithAzureEnvironment(namespace, tokenProvider, env))
	if err != nil {
		return nil, err
	}
	return &HubManager{
		entityManager: newEntityManager(ns.getHTTPSHostURI(), ns.tokenProvider),
	}, nil
}

// Delete deletes an Event Hub entity by name
func (hm *HubManager) Delete(ctx context.Context, name string) error {
	span, ctx := hm.startSpanFromContext(ctx, "eh.HubManager.Delete")
	defer span.End()

	res, err := hm.entityManager.Delete(ctx, "/"+name)
	if res != nil {
		defer res.Body.Close()
	}

	return err
}

// HubWithMessageRetentionInDays configures an Event Hub to retain messages for that number of days
func HubWithMessageRetentionInDays(days int32) HubManagementOption {
	return func(hd *HubDescription) error {
		hd.MessageRetentionInDays = &days
		return nil
	}
}

// HubWithPartitionCount configures an Event Hub to have the specified number of partitions. More partitions == more throughput
func HubWithPartitionCount(count int32) HubManagementOption {
	return func(hd *HubDescription) error {
		hd.PartitionCount = &count
		return nil
	}
}

// Put creates or updates an Event Hubs Hub
func (hm *HubManager) Put(ctx context.Context, name string, opts ...HubManagementOption) (*HubEntity, error) {
	span, ctx := hm.startSpanFromContext(ctx, "eh.HubManager.Put")
	defer span.End()

	hd := new(HubDescription)
	for _, opt := range opts {
		if err := opt(hd); err != nil {
			return nil, err
		}
	}

	hd.ServiceBusSchema = to.StringPtr(serviceBusSchema)

	he := &hubEntry{
		Entry: &atom.Entry{
			AtomSchema: atomSchema,
		},
		Content: &hubContent{
			Type:           applicationXML,
			HubDescription: *hd,
		},
	}

	reqBytes, err := xml.Marshal(he)
	if err != nil {
		tab.For(ctx).Error(err)
		return nil, err
	}

	reqBytes = xmlDoc(reqBytes)
	res, err := hm.entityManager.Put(ctx, "/"+name, reqBytes)
	if res != nil {
		defer res.Body.Close()
	}

	if err != nil {
		tab.For(ctx).Error(err)
		return nil, err
	}

	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		tab.For(ctx).Error(err)
		return nil, err
	}

	var entry hubEntry
	err = xml.Unmarshal(b, &entry)
	if err != nil {
		return nil, formatManagementError(b)
	}
	return hubEntryToEntity(&entry), nil
}

// List fetches all of the Hub for an Event Hubs Namespace
func (hm *HubManager) List(ctx context.Context) ([]*HubEntity, error) {
	span, ctx := hm.startSpanFromContext(ctx, "eh.HubManager.List")
	defer span.End()

	res, err := hm.entityManager.Get(ctx, `/$Resources/EventHubs`)
	if res != nil {
		defer res.Body.Close()
	}

	if err != nil {
		tab.For(ctx).Error(err)
		return nil, err
	}

	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		tab.For(ctx).Error(err)
		return nil, err
	}

	var feed hubFeed
	err = xml.Unmarshal(b, &feed)
	if err != nil {
		return nil, formatManagementError(b)
	}

	qd := make([]*HubEntity, len(feed.Entries))
	for idx, entry := range feed.Entries {
		qd[idx] = hubEntryToEntity(&entry)
	}
	return qd, nil
}

// Get fetches an Event Hubs Hub entity by name
func (hm *HubManager) Get(ctx context.Context, name string) (*HubEntity, error) {
	span, ctx := hm.startSpanFromContext(ctx, "eh.HubManager.Get")
	defer span.End()

	res, err := hm.entityManager.Get(ctx, name)
	if res != nil {
		defer res.Body.Close()
	}

	if err != nil {
		tab.For(ctx).Error(err)
		return nil, err
	}

	if res.StatusCode == http.StatusNotFound {
		return nil, nil
	}

	b, err := ioutil.ReadAll(res.Body)
	if err != nil {
		tab.For(ctx).Error(err)
		return nil, err
	}

	var entry hubEntry
	err = xml.Unmarshal(b, &entry)
	if err != nil {
		if isEmptyFeed(b) {
			return nil, nil
		}
		return nil, formatManagementError(b)
	}

	return hubEntryToEntity(&entry), nil
}

func isEmptyFeed(b []byte) bool {
	var emptyFeed hubFeed
	feedErr := xml.Unmarshal(b, &emptyFeed)
	return feedErr == nil && emptyFeed.Title == "Publicly Listed Services"
}

func hubEntryToEntity(entry *hubEntry) *HubEntity {
	return &HubEntity{
		HubDescription: &entry.Content.HubDescription,
		Name:           entry.Title,
	}
}

// NewHub creates a new Event Hub client for sending and receiving messages
func NewHub(namespace, name string, tokenProvider auth.TokenProvider, opts ...HubOption) (*Hub, error) {
	ns, err := newNamespace(namespaceWithAzureEnvironment(namespace, tokenProvider, azure.PublicCloud))
	if err != nil {
		return nil, err
	}

	h := &Hub{
		name:            name,
		namespace:       ns,
		offsetPersister: persist.NewMemoryPersister(),
		userAgent:       rootUserAgent,
		receivers:       make(map[string]*receiver),
	}

	for _, opt := range opts {
		err := opt(h)
		if err != nil {
			return nil, err
		}
	}

	return h, nil
}

// NewHubWithNamespaceNameAndEnvironment creates a new Event Hub client for sending and receiving messages from
// environment variables with supplied namespace and name which will attempt to build a token provider from
// environment variables. If unable to build a AAD Token Provider it will fall back to a SAS token provider. If neither
// can be built, it will return error.
//
// SAS TokenProvider environment variables:
//
// There are two sets of environment variables which can produce a SAS TokenProvider
//
//   1) Expected Environment Variables:
//     - "EVENTHUB_KEY_NAME" the name of the Event Hub key
//     - "EVENTHUB_KEY_VALUE" the secret for the Event Hub key named in "EVENTHUB_KEY_NAME"
//
//   2) Expected Environment Variable:
//     - "EVENTHUB_CONNECTION_STRING" connection string from the Azure portal
//
//
// AAD TokenProvider environment variables:
//
//   1. client Credentials: attempt to authenticate with a Service Principal via "AZURE_TENANT_ID", "AZURE_CLIENT_ID" and
//     "AZURE_CLIENT_SECRET"
//
//   2. client Certificate: attempt to authenticate with a Service Principal via "AZURE_TENANT_ID", "AZURE_CLIENT_ID",
//     "AZURE_CERTIFICATE_PATH" and "AZURE_CERTIFICATE_PASSWORD"
//
//   3. Managed Service Identity (MSI): attempt to authenticate via MSI on the default local MSI internally addressable IP
//     and port. See: adal.GetMSIVMEndpoint()
//
//
// The Azure Environment used can be specified using the name of the Azure Environment set in the AZURE_ENVIRONMENT var.
func NewHubWithNamespaceNameAndEnvironment(namespace, name string, opts ...HubOption) (*Hub, error) {
	var provider auth.TokenProvider
	provider, sasErr := sas.NewTokenProvider(sas.TokenProviderWithEnvironmentVars())
	if sasErr == nil {
		return NewHub(namespace, name, provider, opts...)
	}

	provider, aadErr := aad.NewJWTProvider(aad.JWTProviderWithEnvironmentVars())
	if aadErr == nil {
		return NewHub(namespace, name, provider, opts...)
	}

	return nil, fmt.Errorf("neither Azure Active Directory nor SAS token provider could be built - AAD error: %v, SAS error: %v", aadErr, sasErr)
}

// NewHubFromEnvironment creates a new Event Hub client for sending and receiving messages from environment variables
//
// Expected Environment Variables:
//   - "EVENTHUB_NAMESPACE" the namespace of the Event Hub instance
//   - "EVENTHUB_NAME" the name of the Event Hub instance
//
//
// This method depends on NewHubWithNamespaceNameAndEnvironment which will attempt to build a token provider from
// environment variables. If unable to build a AAD Token Provider it will fall back to a SAS token provider. If neither
// can be built, it will return error.
//
// SAS TokenProvider environment variables:
//
// There are two sets of environment variables which can produce a SAS TokenProvider
//
//   1) Expected Environment Variables:
//     - "EVENTHUB_NAMESPACE" the namespace of the Event Hub instance
//     - "EVENTHUB_KEY_NAME" the name of the Event Hub key
//     - "EVENTHUB_KEY_VALUE" the secret for the Event Hub key named in "EVENTHUB_KEY_NAME"
//
//   2) Expected Environment Variable:
//     - "EVENTHUB_CONNECTION_STRING" connection string from the Azure portal
//
//
// AAD TokenProvider environment variables:
//   1. client Credentials: attempt to authenticate with a Service Principal via "AZURE_TENANT_ID", "AZURE_CLIENT_ID" and
//     "AZURE_CLIENT_SECRET"
//
//   2. client Certificate: attempt to authenticate with a Service Principal via "AZURE_TENANT_ID", "AZURE_CLIENT_ID",
//     "AZURE_CERTIFICATE_PATH" and "AZURE_CERTIFICATE_PASSWORD"
//
//   3. Managed Service Identity (MSI): attempt to authenticate via MSI
//
//
// The Azure Environment used can be specified using the name of the Azure Environment set in the AZURE_ENVIRONMENT var.
func NewHubFromEnvironment(opts ...HubOption) (*Hub, error) {
	const envErrMsg = "environment var %s must not be empty"
	var namespace, name string

	if namespace = os.Getenv("EVENTHUB_NAMESPACE"); namespace == "" {
		return nil, fmt.Errorf(envErrMsg, "EVENTHUB_NAMESPACE")
	}

	if name = os.Getenv("EVENTHUB_NAME"); name == "" {
		return nil, fmt.Errorf(envErrMsg, "EVENTHUB_NAME")
	}

	return NewHubWithNamespaceNameAndEnvironment(namespace, name, opts...)
}

// NewHubFromConnectionString creates a new Event Hub client for sending and receiving messages from a connection string
// formatted like the following:
//
//   Endpoint=sb://namespace.servicebus.windows.net/;SharedAccessKeyName=RootManageSharedAccessKey;SharedAccessKey=superSecret1234=;EntityPath=hubName
func NewHubFromConnectionString(connStr string, opts ...HubOption) (*Hub, error) {
	parsed, err := conn.ParsedConnectionFromStr(connStr)
	if err != nil {
		return nil, err
	}

	ns, err := newNamespace(namespaceWithConnectionString(connStr))
	if err != nil {
		return nil, err
	}

	h := &Hub{
		name:            parsed.HubName,
		namespace:       ns,
		offsetPersister: persist.NewMemoryPersister(),
		userAgent:       rootUserAgent,
		receivers:       make(map[string]*receiver),
	}

	for _, opt := range opts {
		err := opt(h)
		if err != nil {
			return nil, err
		}
	}

	return h, err
}

// GetRuntimeInformation fetches runtime information from the Event Hub management node
func (h *Hub) GetRuntimeInformation(ctx context.Context) (*HubRuntimeInformation, error) {
	span, ctx := h.startSpanFromContext(ctx, "eh.Hub.GetRuntimeInformation")
	defer span.End()
	client := newClient(h.namespace, h.name)
	c, err := h.namespace.newConnection()
	if err != nil {
		tab.For(ctx).Error(err)
		return nil, err
	}

	defer func() {
		if err := c.Close(); err != nil {
			tab.For(ctx).Error(err)
		}
	}()

	info, err := client.GetHubRuntimeInformation(ctx, c)
	if err != nil {
		tab.For(ctx).Error(err)
		return nil, err
	}

	return info, nil
}

// GetPartitionInformation fetches runtime information about a specific partition from the Event Hub management node
func (h *Hub) GetPartitionInformation(ctx context.Context, partitionID string) (*HubPartitionRuntimeInformation, error) {
	span, ctx := h.startSpanFromContext(ctx, "eh.Hub.GetPartitionInformation")
	defer span.End()
	client := newClient(h.namespace, h.name)
	c, err := h.namespace.newConnection()
	if err != nil {
		tab.For(ctx).Error(err)
		return nil, err
	}

	defer func() {
		if err := c.Close(); err != nil {
			tab.For(ctx).Error(err)
		}
	}()

	info, err := client.GetHubPartitionRuntimeInformation(ctx, c, partitionID)
	if err != nil {
		return nil, err
	}

	return info, nil
}

// Close drains and closes all of the existing senders, receivers and connections
func (h *Hub) Close(ctx context.Context) error {
	span, ctx := h.startSpanFromContext(ctx, "eh.Hub.Close")
	defer span.End()

	if h.sender != nil {
		if err := h.sender.Close(ctx); err != nil {
			if rErr := h.closeReceivers(ctx); rErr != nil {
				if !isConnectionClosed(rErr) {
					tab.For(ctx).Error(rErr)
				}
			}

			if !isConnectionClosed(err) {
				tab.For(ctx).Error(err)
				return err
			}

			return nil
		}
	}

	// close receivers and return error
	err := h.closeReceivers(ctx)
	if err != nil && !isConnectionClosed(err) {
		tab.For(ctx).Error(err)
		return err
	}

	return nil
}

// closeReceivers will close the receivers on the hub and return the last error
func (h *Hub) closeReceivers(ctx context.Context) error {
	span, ctx := h.startSpanFromContext(ctx, "eh.Hub.closeReceivers")
	defer span.End()

	var lastErr error
	for _, r := range h.receivers {
		if err := r.Close(ctx); err != nil {
			tab.For(ctx).Error(err)
			lastErr = err
		}
	}
	return lastErr
}

// Receive subscribes for messages sent to the provided entityPath.
//
// The context passed into Receive is only used to limit the amount of time the caller will wait for the Receive
// method to connect to the Event Hub. The context passed in does not control the lifetime of Receive after connection.
//
// If Receive encounters an initial error setting up the connection, an error will be returned.
//
// If Receive starts successfully, a *ListenerHandle and a nil error will be returned. The ListenerHandle exposes
// methods which will help manage the life span of the receiver.
//
// ListenerHandle.Close(ctx) closes the receiver
//
// ListenerHandle.Done() signals the consumer when the receiver has stopped
//
// ListenerHandle.Err() provides the last error the listener encountered and was unable to recover from
func (h *Hub) Receive(ctx context.Context, partitionID string, handler Handler, opts ...ReceiveOption) (*ListenerHandle, error) {
	span, ctx := h.startSpanFromContext(ctx, "eh.Hub.Receive")
	defer span.End()

	h.receiverMu.Lock()
	defer h.receiverMu.Unlock()

	receiver, err := h.newReceiver(ctx, partitionID, opts...)
	if err != nil {
		return nil, err
	}

	// Todo: change this to use name rather than identifier
	if r, ok := h.receivers[receiver.getIdentifier()]; ok {
		if err := r.Close(ctx); err != nil {
			tab.For(ctx).Error(err)
		}
	}

	h.receivers[receiver.getIdentifier()] = receiver
	listenerContext := receiver.Listen(handler)

	return listenerContext, nil
}

// Send sends an event to the Event Hub
//
// Send will retry sending the message for as long as the context allows
func (h *Hub) Send(ctx context.Context, event *Event, opts ...SendOption) error {
	span, ctx := h.startSpanFromContext(ctx, "eh.Hub.Send")
	defer span.End()

	sender, err := h.getSender(ctx)
	if err != nil {
		return err
	}

	return sender.Send(ctx, event, opts...)
}

// SendBatch sends a batch of events to the Hub
func (h *Hub) SendBatch(ctx context.Context, iterator BatchIterator, opts ...BatchOption) error {
	span, ctx := h.startSpanFromContext(ctx, "eh.Hub.SendBatch")
	defer span.End()

	sender, err := h.getSender(ctx)
	if err != nil {
		tab.For(ctx).Error(err)
		return err
	}

	batchOptions := &BatchOptions{
		MaxSize: DefaultMaxMessageSizeInBytes,
	}

	for _, opt := range opts {
		if err := opt(batchOptions); err != nil {
			tab.For(ctx).Error(err)
			return err
		}
	}

	for !iterator.Done() {
		id, err := uuid.NewV4()
		if err != nil {
			tab.For(ctx).Error(err)
			return err
		}

		batch, err := iterator.Next(id.String(), batchOptions)

		if err != nil {
			tab.For(ctx).Error(err)
			return err
		}

		if err := sender.trySend(ctx, batch); err != nil {
			tab.For(ctx).Error(err)
			return err
		}
	}

	return nil
}

// HubWithPartitionedSender configures the Hub instance to send to a specific event Hub partition
func HubWithPartitionedSender(partitionID string) HubOption {
	return func(h *Hub) error {
		h.senderPartitionID = &partitionID
		return nil
	}
}

// HubWithOffsetPersistence configures the Hub instance to read and write offsets so that if a Hub is interrupted, it
// can resume after the last consumed event.
func HubWithOffsetPersistence(offsetPersister persist.CheckpointPersister) HubOption {
	return func(h *Hub) error {
		h.offsetPersister = offsetPersister
		return nil
	}
}

// HubWithUserAgent configures the Hub to append the given string to the user agent sent to the server
//
// This option can be specified multiple times to add additional segments.
//
// Max user agent length is specified by the const maxUserAgentLen.
func HubWithUserAgent(userAgent string) HubOption {
	return func(h *Hub) error {
		return h.appendAgent(userAgent)
	}
}

// HubWithEnvironment configures the Hub to use the specified environment.
//
// By default, the Hub instance will use Azure US Public cloud environment
func HubWithEnvironment(env azure.Environment) HubOption {
	return func(h *Hub) error {
		h.namespace.host = "amqps://" + h.namespace.name + "." + env.ServiceBusEndpointSuffix
		return nil
	}
}

// HubWithWebSocketConnection configures the Hub to use a WebSocket connection wss:// rather than amqps://
func HubWithWebSocketConnection() HubOption {
	return func(h *Hub) error {
		h.namespace.useWebSocket = true
		return nil
	}
}

func (h *Hub) appendAgent(userAgent string) error {
	ua := path.Join(h.userAgent, userAgent)
	if len(ua) > maxUserAgentLen {
		return fmt.Errorf("user agent string has surpassed the max length of %d", maxUserAgentLen)
	}
	h.userAgent = ua
	return nil
}

func (h *Hub) getSender(ctx context.Context) (*sender, error) {
	h.senderMu.Lock()
	defer h.senderMu.Unlock()

	span, ctx := h.startSpanFromContext(ctx, "eh.Hub.getSender")
	defer span.End()

	if h.sender == nil {
		s, err := h.newSender(ctx)
		if err != nil {
			tab.For(ctx).Error(err)
			return nil, err
		}
		h.sender = s
	}
	return h.sender, nil
}

func isRecoverableCloseError(err error) bool {
	return isConnectionClosed(err) || isSessionClosed(err) || isLinkClosed(err)
}

func isConnectionClosed(err error) bool {
	return err == amqp.ErrConnClosed
}

func isLinkClosed(err error) bool {
	return err == amqp.ErrLinkClosed
}

func isSessionClosed(err error) bool {
	return err == amqp.ErrSessionClosed
}

// Copyright 2021 Google LLC
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
	"context"
	"fmt"
	"time"

	"google.golang.org/api/option"

	vkit "cloud.google.com/go/pubsub/apiv1"
	pb "cloud.google.com/go/pubsub/apiv1/pubsubpb"
)

// SchemaClient is a Pub/Sub schema client scoped to a single project.
type SchemaClient struct {
	sc        *vkit.SchemaClient
	projectID string
}

// Close closes the schema client and frees up resources.
func (s *SchemaClient) Close() error {
	return s.sc.Close()
}

// NewSchemaClient creates a new Pub/Sub Schema client.
func NewSchemaClient(ctx context.Context, projectID string, opts ...option.ClientOption) (*SchemaClient, error) {
	sc, err := vkit.NewSchemaClient(ctx, opts...)
	if err != nil {
		return nil, err
	}
	return &SchemaClient{sc: sc, projectID: projectID}, nil
}

// SchemaConfig is a reference to a PubSub schema.
type SchemaConfig struct {
	// Name of the schema.
	// Format is `projects/{project}/schemas/{schema}`
	Name string

	// The type of the schema definition.
	Type SchemaType

	// The definition of the schema. This should contain a string representing
	// the full definition of the schema that is a valid schema definition of
	// the type specified in `type`.
	Definition string

	// RevisionID is the revision ID of the schema.
	// This field is output only.
	RevisionID string

	// RevisionCreateTime is the timestamp that the revision was created.
	// This field is output only.
	RevisionCreateTime time.Time
}

// SchemaType is the possible schema definition types.
type SchemaType pb.Schema_Type

const (
	// SchemaTypeUnspecified is the unused default value.
	SchemaTypeUnspecified SchemaType = 0
	// SchemaProtocolBuffer is a protobuf schema definition.
	SchemaProtocolBuffer SchemaType = 1
	// SchemaAvro is an Avro schema definition.
	SchemaAvro SchemaType = 2
)

// SchemaView is a view of Schema object fields to be returned
// by GetSchema and ListSchemas.
type SchemaView pb.SchemaView

const (
	// SchemaViewUnspecified is the default/unset value.
	SchemaViewUnspecified SchemaView = 0
	// SchemaViewBasic includes the name and type of the schema, but not the definition.
	SchemaViewBasic SchemaView = 1
	// SchemaViewFull includes all Schema object fields.
	SchemaViewFull SchemaView = 2
)

// SchemaSettings are settings for validating messages
// published against a schema.
type SchemaSettings struct {
	// The name of the schema that messages published should be
	// validated against. Format is `projects/{project}/schemas/{schema}`
	Schema string

	// The encoding of messages validated against the schema.
	Encoding SchemaEncoding

	// The minimum (inclusive) revision allowed for validating messages. If empty
	// or not present, allow any revision to be validated against LastRevisionID or
	// any revision created before.
	FirstRevisionID string

	// The maximum (inclusive) revision allowed for validating messages. If empty
	// or not present, allow any revision to be validated against FirstRevisionID
	// or any revision created after.
	LastRevisionID string
}

func schemaSettingsToProto(schema *SchemaSettings) *pb.SchemaSettings {
	if schema == nil {
		return nil
	}
	return &pb.SchemaSettings{
		Schema:          schema.Schema,
		Encoding:        pb.Encoding(schema.Encoding),
		FirstRevisionId: schema.FirstRevisionID,
		LastRevisionId:  schema.LastRevisionID,
	}
}

func protoToSchemaSettings(pbs *pb.SchemaSettings) *SchemaSettings {
	if pbs == nil {
		return nil
	}
	return &SchemaSettings{
		Schema:          pbs.Schema,
		Encoding:        SchemaEncoding(pbs.Encoding),
		FirstRevisionID: pbs.FirstRevisionId,
		LastRevisionID:  pbs.LastRevisionId,
	}
}

// SchemaEncoding is the encoding expected for messages.
type SchemaEncoding pb.Encoding

const (
	// EncodingUnspecified is the default unused value.
	EncodingUnspecified SchemaEncoding = 0
	// EncodingJSON is the JSON encoding type for a message.
	EncodingJSON SchemaEncoding = 1
	// EncodingBinary is the binary encoding type for a message.
	// For some schema types, binary encoding may not be available.
	EncodingBinary SchemaEncoding = 2
)

func (s *SchemaConfig) toProto() *pb.Schema {
	pbs := &pb.Schema{
		Name:       s.Name,
		Type:       pb.Schema_Type(s.Type),
		Definition: s.Definition,
	}
	return pbs
}

func protoToSchemaConfig(pbs *pb.Schema) *SchemaConfig {
	return &SchemaConfig{
		Name:               pbs.Name,
		Type:               SchemaType(pbs.Type),
		Definition:         pbs.Definition,
		RevisionID:         pbs.RevisionId,
		RevisionCreateTime: pbs.RevisionCreateTime.AsTime(),
	}
}

// CreateSchema creates a new schema with the given schemaID
// and config. Schemas cannot be updated after creation.
func (c *SchemaClient) CreateSchema(ctx context.Context, schemaID string, s SchemaConfig) (*SchemaConfig, error) {
	req := &pb.CreateSchemaRequest{
		Parent:   fmt.Sprintf("projects/%s", c.projectID),
		Schema:   s.toProto(),
		SchemaId: schemaID,
	}
	pbs, err := c.sc.CreateSchema(ctx, req)
	if err != nil {
		return nil, err
	}
	return protoToSchemaConfig(pbs), nil
}

// Schema retrieves the configuration of a schema given a schemaID and a view.
func (c *SchemaClient) Schema(ctx context.Context, schemaID string, view SchemaView) (*SchemaConfig, error) {
	schemaPath := fmt.Sprintf("projects/%s/schemas/%s", c.projectID, schemaID)
	req := &pb.GetSchemaRequest{
		Name: schemaPath,
		View: pb.SchemaView(view),
	}
	s, err := c.sc.GetSchema(ctx, req)
	if err != nil {
		return nil, err
	}
	return protoToSchemaConfig(s), nil
}

// Schemas returns an iterator which returns all of the schemas for the client's project.
func (c *SchemaClient) Schemas(ctx context.Context, view SchemaView) *SchemaIterator {
	return &SchemaIterator{
		it: c.sc.ListSchemas(ctx, &pb.ListSchemasRequest{
			Parent: fmt.Sprintf("projects/%s", c.projectID),
			View:   pb.SchemaView(view),
		}),
	}
}

// SchemaIterator is a struct used to iterate over schemas.
type SchemaIterator struct {
	it  *vkit.SchemaIterator
	err error
}

// Next returns the next schema. If there are no more schemas, iterator.Done will be returned.
func (s *SchemaIterator) Next() (*SchemaConfig, error) {
	if s.err != nil {
		return nil, s.err
	}
	pbs, err := s.it.Next()
	if err != nil {
		return nil, err
	}
	return protoToSchemaConfig(pbs), nil
}

// ListSchemaRevisions lists all schema revisions for the named schema.
func (c *SchemaClient) ListSchemaRevisions(ctx context.Context, schemaID string, view SchemaView) *SchemaIterator {
	return &SchemaIterator{
		it: c.sc.ListSchemaRevisions(ctx, &pb.ListSchemaRevisionsRequest{
			Name: fmt.Sprintf("projects/%s/schemas/%s", c.projectID, schemaID),
			View: pb.SchemaView(view),
		}),
	}
}

// CommitSchema commits a new schema revision to an existing schema.
func (c *SchemaClient) CommitSchema(ctx context.Context, schemaID string, s SchemaConfig) (*SchemaConfig, error) {
	req := &pb.CommitSchemaRequest{
		Name:   fmt.Sprintf("projects/%s/schemas/%s", c.projectID, schemaID),
		Schema: s.toProto(),
	}
	pbs, err := c.sc.CommitSchema(ctx, req)
	if err != nil {
		return nil, err
	}
	return protoToSchemaConfig(pbs), nil
}

// RollbackSchema creates a new schema revision that is a copy of the provided revision.
func (c *SchemaClient) RollbackSchema(ctx context.Context, schemaID, revisionID string) (*SchemaConfig, error) {
	req := &pb.RollbackSchemaRequest{
		Name:       fmt.Sprintf("projects/%s/schemas/%s", c.projectID, schemaID),
		RevisionId: revisionID,
	}
	pbs, err := c.sc.RollbackSchema(ctx, req)
	if err != nil {
		return nil, err
	}
	return protoToSchemaConfig(pbs), nil
}

// DeleteSchemaRevision deletes a specific schema revision.
func (c *SchemaClient) DeleteSchemaRevision(ctx context.Context, schemaID, revisionID string) (*SchemaConfig, error) {
	schemaPath := fmt.Sprintf("projects/%s/schemas/%s@%s", c.projectID, schemaID, revisionID)
	schema, err := c.sc.DeleteSchemaRevision(ctx, &pb.DeleteSchemaRevisionRequest{
		Name: schemaPath,
	})
	if err != nil {
		return nil, err
	}
	return protoToSchemaConfig(schema), nil
}

// DeleteSchema deletes an existing schema given a schema ID.
func (c *SchemaClient) DeleteSchema(ctx context.Context, schemaID string) error {
	schemaPath := fmt.Sprintf("projects/%s/schemas/%s", c.projectID, schemaID)
	return c.sc.DeleteSchema(ctx, &pb.DeleteSchemaRequest{
		Name: schemaPath,
	})
}

// ValidateSchemaResult is the response for the ValidateSchema method.
// Reserved for future use.
type ValidateSchemaResult struct{}

// ValidateSchema validates a schema config and returns an error if invalid.
func (c *SchemaClient) ValidateSchema(ctx context.Context, schema SchemaConfig) (*ValidateSchemaResult, error) {
	req := &pb.ValidateSchemaRequest{
		Parent: fmt.Sprintf("projects/%s", c.projectID),
		Schema: schema.toProto(),
	}
	_, err := c.sc.ValidateSchema(ctx, req)
	if err != nil {
		return nil, err
	}
	return &ValidateSchemaResult{}, nil
}

// ValidateMessageResult is the response for the ValidateMessage method.
// Reserved for future use.
type ValidateMessageResult struct{}

// ValidateMessageWithConfig validates a message against an schema specified
// by a schema config.
func (c *SchemaClient) ValidateMessageWithConfig(ctx context.Context, msg []byte, encoding SchemaEncoding, config SchemaConfig) (*ValidateMessageResult, error) {
	req := &pb.ValidateMessageRequest{
		Parent: fmt.Sprintf("projects/%s", c.projectID),
		SchemaSpec: &pb.ValidateMessageRequest_Schema{
			Schema: config.toProto(),
		},
		Message:  msg,
		Encoding: pb.Encoding(encoding),
	}
	_, err := c.sc.ValidateMessage(ctx, req)
	if err != nil {
		return nil, err
	}
	return &ValidateMessageResult{}, nil
}

// ValidateMessageWithID validates a message against an schema specified
// by the schema ID of an existing schema.
func (c *SchemaClient) ValidateMessageWithID(ctx context.Context, msg []byte, encoding SchemaEncoding, schemaID string) (*ValidateMessageResult, error) {
	req := &pb.ValidateMessageRequest{
		Parent: fmt.Sprintf("projects/%s", c.projectID),
		SchemaSpec: &pb.ValidateMessageRequest_Name{
			Name: fmt.Sprintf("projects/%s/schemas/%s", c.projectID, schemaID),
		},
		Message:  msg,
		Encoding: pb.Encoding(encoding),
	}
	_, err := c.sc.ValidateMessage(ctx, req)
	if err != nil {
		return nil, err
	}
	return &ValidateMessageResult{}, nil
}

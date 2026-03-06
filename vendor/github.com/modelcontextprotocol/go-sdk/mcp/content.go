// Copyright 2025 The Go MCP SDK Authors. All rights reserved.
// Use of this source code is governed by an MIT-style
// license that can be found in the LICENSE file.

// TODO(findleyr): update JSON marshalling of all content types to preserve required fields.
// (See [TextContent.MarshalJSON], which handles this for text content).

package mcp

import (
	"encoding/json"
	"errors"
	"fmt"
)

// A Content is a [TextContent], [ImageContent], [AudioContent],
// [ResourceLink], or [EmbeddedResource].
type Content interface {
	MarshalJSON() ([]byte, error)
	fromWire(*wireContent)
}

// TextContent is a textual content.
type TextContent struct {
	Text        string
	Meta        Meta
	Annotations *Annotations
}

func (c *TextContent) MarshalJSON() ([]byte, error) {
	// Custom wire format to ensure the required "text" field is always included, even when empty.
	wire := struct {
		Type        string       `json:"type"`
		Text        string       `json:"text"`
		Meta        Meta         `json:"_meta,omitempty"`
		Annotations *Annotations `json:"annotations,omitempty"`
	}{
		Type:        "text",
		Text:        c.Text,
		Meta:        c.Meta,
		Annotations: c.Annotations,
	}
	return json.Marshal(wire)
}

func (c *TextContent) fromWire(wire *wireContent) {
	c.Text = wire.Text
	c.Meta = wire.Meta
	c.Annotations = wire.Annotations
}

// ImageContent contains base64-encoded image data.
type ImageContent struct {
	Meta        Meta
	Annotations *Annotations
	Data        []byte // base64-encoded
	MIMEType    string
}

func (c *ImageContent) MarshalJSON() ([]byte, error) {
	// Custom wire format to ensure required fields are always included, even when empty.
	data := c.Data
	if data == nil {
		data = []byte{}
	}
	wire := imageAudioWire{
		Type:        "image",
		MIMEType:    c.MIMEType,
		Data:        data,
		Meta:        c.Meta,
		Annotations: c.Annotations,
	}
	return json.Marshal(wire)
}

func (c *ImageContent) fromWire(wire *wireContent) {
	c.MIMEType = wire.MIMEType
	c.Data = wire.Data
	c.Meta = wire.Meta
	c.Annotations = wire.Annotations
}

// AudioContent contains base64-encoded audio data.
type AudioContent struct {
	Data        []byte
	MIMEType    string
	Meta        Meta
	Annotations *Annotations
}

func (c AudioContent) MarshalJSON() ([]byte, error) {
	// Custom wire format to ensure required fields are always included, even when empty.
	data := c.Data
	if data == nil {
		data = []byte{}
	}
	wire := imageAudioWire{
		Type:        "audio",
		MIMEType:    c.MIMEType,
		Data:        data,
		Meta:        c.Meta,
		Annotations: c.Annotations,
	}
	return json.Marshal(wire)
}

func (c *AudioContent) fromWire(wire *wireContent) {
	c.MIMEType = wire.MIMEType
	c.Data = wire.Data
	c.Meta = wire.Meta
	c.Annotations = wire.Annotations
}

// Custom wire format to ensure required fields are always included, even when empty.
type imageAudioWire struct {
	Type        string       `json:"type"`
	MIMEType    string       `json:"mimeType"`
	Data        []byte       `json:"data"`
	Meta        Meta         `json:"_meta,omitempty"`
	Annotations *Annotations `json:"annotations,omitempty"`
}

// ResourceLink is a link to a resource
type ResourceLink struct {
	URI         string
	Name        string
	Title       string
	Description string
	MIMEType    string
	Size        *int64
	Meta        Meta
	Annotations *Annotations
	// Icons for the resource link, if any.
	Icons []Icon `json:"icons,omitempty"`
}

func (c *ResourceLink) MarshalJSON() ([]byte, error) {
	return json.Marshal(&wireContent{
		Type:        "resource_link",
		URI:         c.URI,
		Name:        c.Name,
		Title:       c.Title,
		Description: c.Description,
		MIMEType:    c.MIMEType,
		Size:        c.Size,
		Meta:        c.Meta,
		Annotations: c.Annotations,
		Icons:       c.Icons,
	})
}

func (c *ResourceLink) fromWire(wire *wireContent) {
	c.URI = wire.URI
	c.Name = wire.Name
	c.Title = wire.Title
	c.Description = wire.Description
	c.MIMEType = wire.MIMEType
	c.Size = wire.Size
	c.Meta = wire.Meta
	c.Annotations = wire.Annotations
	c.Icons = wire.Icons
}

// EmbeddedResource contains embedded resources.
type EmbeddedResource struct {
	Resource    *ResourceContents
	Meta        Meta
	Annotations *Annotations
}

func (c *EmbeddedResource) MarshalJSON() ([]byte, error) {
	return json.Marshal(&wireContent{
		Type:        "resource",
		Resource:    c.Resource,
		Meta:        c.Meta,
		Annotations: c.Annotations,
	})
}

func (c *EmbeddedResource) fromWire(wire *wireContent) {
	c.Resource = wire.Resource
	c.Meta = wire.Meta
	c.Annotations = wire.Annotations
}

// ResourceContents contains the contents of a specific resource or
// sub-resource.
type ResourceContents struct {
	URI      string `json:"uri"`
	MIMEType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     []byte `json:"blob,omitempty"`
	Meta     Meta   `json:"_meta,omitempty"`
}

func (r *ResourceContents) MarshalJSON() ([]byte, error) {
	// If we could assume Go 1.24, we could use omitzero for Blob and avoid this method.
	if r.URI == "" {
		return nil, errors.New("ResourceContents missing URI")
	}
	if r.Blob == nil {
		// Text. Marshal normally.
		type wireResourceContents ResourceContents // (lacks MarshalJSON method)
		return json.Marshal((wireResourceContents)(*r))
	}
	// Blob.
	if r.Text != "" {
		return nil, errors.New("ResourceContents has non-zero Text and Blob fields")
	}
	// r.Blob may be the empty slice, so marshal with an alternative definition.
	br := struct {
		URI      string `json:"uri,omitempty"`
		MIMEType string `json:"mimeType,omitempty"`
		Blob     []byte `json:"blob"`
		Meta     Meta   `json:"_meta,omitempty"`
	}{
		URI:      r.URI,
		MIMEType: r.MIMEType,
		Blob:     r.Blob,
		Meta:     r.Meta,
	}
	return json.Marshal(br)
}

// wireContent is the wire format for content.
// It represents the protocol types TextContent, ImageContent, AudioContent,
// ResourceLink, and EmbeddedResource.
// The Type field distinguishes them. In the protocol, each type has a constant
// value for the field.
// At most one of Text, Data, Resource, and URI is non-zero.
type wireContent struct {
	Type        string            `json:"type"`
	Text        string            `json:"text,omitempty"`
	MIMEType    string            `json:"mimeType,omitempty"`
	Data        []byte            `json:"data,omitempty"`
	Resource    *ResourceContents `json:"resource,omitempty"`
	URI         string            `json:"uri,omitempty"`
	Name        string            `json:"name,omitempty"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description,omitempty"`
	Size        *int64            `json:"size,omitempty"`
	Meta        Meta              `json:"_meta,omitempty"`
	Annotations *Annotations      `json:"annotations,omitempty"`
	Icons       []Icon            `json:"icons,omitempty"`
}

func contentsFromWire(wires []*wireContent, allow map[string]bool) ([]Content, error) {
	var blocks []Content
	for _, wire := range wires {
		block, err := contentFromWire(wire, allow)
		if err != nil {
			return nil, err
		}
		blocks = append(blocks, block)
	}
	return blocks, nil
}

func contentFromWire(wire *wireContent, allow map[string]bool) (Content, error) {
	if wire == nil {
		return nil, fmt.Errorf("nil content")
	}
	if allow != nil && !allow[wire.Type] {
		return nil, fmt.Errorf("invalid content type %q", wire.Type)
	}
	switch wire.Type {
	case "text":
		v := new(TextContent)
		v.fromWire(wire)
		return v, nil
	case "image":
		v := new(ImageContent)
		v.fromWire(wire)
		return v, nil
	case "audio":
		v := new(AudioContent)
		v.fromWire(wire)
		return v, nil
	case "resource_link":
		v := new(ResourceLink)
		v.fromWire(wire)
		return v, nil
	case "resource":
		v := new(EmbeddedResource)
		v.fromWire(wire)
		return v, nil
	}
	return nil, fmt.Errorf("internal error: unrecognized content type %s", wire.Type)
}

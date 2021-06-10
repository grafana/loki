// Package atom contains base data structures for use in the Azure Event Hubs management HTTP API
package atom

import (
	"encoding/xml"

	"github.com/Azure/go-autorest/autorest/date"
)

type (
	// Feed is an Atom feed which contains entries
	Feed struct {
		XMLName xml.Name   `xml:"feed"`
		ID      string     `xml:"id"`
		Title   string     `xml:"title"`
		Updated *date.Time `xml:"updated,omitempty"`
		Entries []Entry    `xml:"entry"`
	}

	// Entry is the Atom wrapper for a management request
	Entry struct {
		XMLName                   xml.Name   `xml:"entry"`
		ID                        string     `xml:"id,omitempty"`
		Title                     string     `xml:"title,omitempty"`
		Published                 *date.Time `xml:"published,omitempty"`
		Updated                   *date.Time `xml:"updated,omitempty"`
		Author                    *Author    `xml:"author,omitempty"`
		Link                      *Link      `xml:"link,omitempty"`
		Content                   *Content   `xml:"content"`
		DataServiceSchema         string     `xml:"xmlns:d,attr,omitempty"`
		DataServiceMetadataSchema string     `xml:"xmlns:m,attr,omitempty"`
		AtomSchema                string     `xml:"xmlns,attr"`
	}

	// Author is an Atom author used in an entry
	Author struct {
		XMLName xml.Name `xml:"author"`
		Name    *string  `xml:"name,omitempty"`
	}

	// Link is an Atom link used in an entry
	Link struct {
		XMLName xml.Name `xml:"link"`
		Rel     string   `xml:"rel,attr"`
		HREF    string   `xml:"href,attr"`
	}

	// Content is a generic body for an Atom entry
	Content struct {
		XMLName xml.Name `xml:"content"`
		Type    string   `xml:"type,attr"`
		Body    string   `xml:",innerxml"`
	}
)

/*
 * MinIO Go Library for Amazon S3 Compatible Cloud Storage
 * Copyright 2020-2022 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package tags

import (
	"encoding/xml"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"
)

// Error contains tag specific error.
type Error interface {
	error
	Code() string
}

type errTag struct {
	code    string
	message string
}

// Code contains error code.
func (err errTag) Code() string {
	return err.code
}

// Error contains error message.
func (err errTag) Error() string {
	return err.message
}

var (
	errTooManyObjectTags = &errTag{"BadRequest", "Tags cannot be more than 10"}
	errTooManyTags       = &errTag{"BadRequest", "Tags cannot be more than 50"}
	errInvalidTagKey     = &errTag{"InvalidTag", "The TagKey you have provided is invalid"}
	errInvalidTagValue   = &errTag{"InvalidTag", "The TagValue you have provided is invalid"}
	errDuplicateTagKey   = &errTag{"InvalidTag", "Cannot provide multiple Tags with the same key"}
)

// Tag comes with limitation as per
// https://docs.aws.amazon.com/AmazonS3/latest/dev/object-tagging.html amd
// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/Using_Tags.html#tag-restrictions
const (
	maxKeyLength      = 128
	maxValueLength    = 256
	maxObjectTagCount = 10
	maxTagCount       = 50
)

// https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/Using_Tags.html#tag-restrictions
// borrowed from this article and also testing various ASCII characters following regex
// is supported by AWS S3 for both tags and values.
var validTagKeyValue = regexp.MustCompile(`^[a-zA-Z0-9-+\-._:/@ =]+$`)

func checkKey(key string) error {
	if len(key) == 0 {
		return errInvalidTagKey
	}

	if utf8.RuneCountInString(key) > maxKeyLength || !validTagKeyValue.MatchString(key) {
		return errInvalidTagKey
	}

	return nil
}

func checkValue(value string) error {
	if value != "" {
		if utf8.RuneCountInString(value) > maxValueLength || !validTagKeyValue.MatchString(value) {
			return errInvalidTagValue
		}
	}

	return nil
}

// Tag denotes key and value.
type Tag struct {
	Key   string `xml:"Key"`
	Value string `xml:"Value"`
}

func (tag Tag) String() string {
	return tag.Key + "=" + tag.Value
}

// IsEmpty returns whether this tag is empty or not.
func (tag Tag) IsEmpty() bool {
	return tag.Key == ""
}

// Validate checks this tag.
func (tag Tag) Validate() error {
	if err := checkKey(tag.Key); err != nil {
		return err
	}

	return checkValue(tag.Value)
}

// MarshalXML encodes to XML data.
func (tag Tag) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	if err := tag.Validate(); err != nil {
		return err
	}

	type subTag Tag // to avoid recursively calling MarshalXML()
	return e.EncodeElement(subTag(tag), start)
}

// UnmarshalXML decodes XML data to tag.
func (tag *Tag) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	type subTag Tag // to avoid recursively calling UnmarshalXML()
	var st subTag
	if err := d.DecodeElement(&st, &start); err != nil {
		return err
	}

	if err := Tag(st).Validate(); err != nil {
		return err
	}

	*tag = Tag(st)
	return nil
}

// tagSet represents list of unique tags.
type tagSet struct {
	tagMap   map[string]string
	isObject bool
}

func (tags tagSet) String() string {
	if len(tags.tagMap) == 0 {
		return ""
	}
	var buf strings.Builder
	keys := make([]string, 0, len(tags.tagMap))
	for k := range tags.tagMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		keyEscaped := url.QueryEscape(k)
		valueEscaped := url.QueryEscape(tags.tagMap[k])
		if buf.Len() > 0 {
			buf.WriteByte('&')
		}
		buf.WriteString(keyEscaped)
		buf.WriteByte('=')
		buf.WriteString(valueEscaped)
	}
	return buf.String()
}

func (tags *tagSet) remove(key string) {
	delete(tags.tagMap, key)
}

func (tags *tagSet) set(key, value string, failOnExist bool) error {
	if failOnExist {
		if _, found := tags.tagMap[key]; found {
			return errDuplicateTagKey
		}
	}

	if err := checkKey(key); err != nil {
		return err
	}

	if err := checkValue(value); err != nil {
		return err
	}

	if tags.isObject {
		if len(tags.tagMap) == maxObjectTagCount {
			return errTooManyObjectTags
		}
	} else if len(tags.tagMap) == maxTagCount {
		return errTooManyTags
	}

	tags.tagMap[key] = value
	return nil
}

func (tags tagSet) count() int {
	return len(tags.tagMap)
}

func (tags tagSet) toMap() map[string]string {
	m := make(map[string]string, len(tags.tagMap))
	for key, value := range tags.tagMap {
		m[key] = value
	}
	return m
}

// MarshalXML encodes to XML data.
func (tags tagSet) MarshalXML(e *xml.Encoder, start xml.StartElement) error {
	tagList := struct {
		Tags []Tag `xml:"Tag"`
	}{}

	tagList.Tags = make([]Tag, 0, len(tags.tagMap))
	for key, value := range tags.tagMap {
		tagList.Tags = append(tagList.Tags, Tag{key, value})
	}

	return e.EncodeElement(tagList, start)
}

// UnmarshalXML decodes XML data to tag list.
func (tags *tagSet) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	tagList := struct {
		Tags []Tag `xml:"Tag"`
	}{}

	if err := d.DecodeElement(&tagList, &start); err != nil {
		return err
	}

	if tags.isObject {
		if len(tagList.Tags) > maxObjectTagCount {
			return errTooManyObjectTags
		}
	} else if len(tagList.Tags) > maxTagCount {
		return errTooManyTags
	}

	m := make(map[string]string, len(tagList.Tags))
	for _, tag := range tagList.Tags {
		if _, found := m[tag.Key]; found {
			return errDuplicateTagKey
		}

		m[tag.Key] = tag.Value
	}

	tags.tagMap = m
	return nil
}

type tagging struct {
	XMLName xml.Name `xml:"Tagging"`
	TagSet  *tagSet  `xml:"TagSet"`
}

// Tags is list of tags of XML request/response as per
// https://docs.aws.amazon.com/AmazonS3/latest/API/API_GetBucketTagging.html#API_GetBucketTagging_RequestBody
type Tags tagging

func (tags Tags) String() string {
	return tags.TagSet.String()
}

// Remove removes a tag by its key.
func (tags *Tags) Remove(key string) {
	tags.TagSet.remove(key)
}

// Set sets new tag.
func (tags *Tags) Set(key, value string) error {
	return tags.TagSet.set(key, value, false)
}

// Count - return number of tags accounted for
func (tags Tags) Count() int {
	return tags.TagSet.count()
}

// ToMap returns copy of tags.
func (tags Tags) ToMap() map[string]string {
	return tags.TagSet.toMap()
}

// MapToObjectTags converts an input map of key and value into
// *Tags data structure with validation.
func MapToObjectTags(tagMap map[string]string) (*Tags, error) {
	return NewTags(tagMap, true)
}

// MapToBucketTags converts an input map of key and value into
// *Tags data structure with validation.
func MapToBucketTags(tagMap map[string]string) (*Tags, error) {
	return NewTags(tagMap, false)
}

// NewTags creates Tags from tagMap, If isObject is set, it validates for object tags.
func NewTags(tagMap map[string]string, isObject bool) (*Tags, error) {
	tagging := &Tags{
		TagSet: &tagSet{
			tagMap:   make(map[string]string),
			isObject: isObject,
		},
	}

	for key, value := range tagMap {
		if err := tagging.TagSet.set(key, value, true); err != nil {
			return nil, err
		}
	}

	return tagging, nil
}

func unmarshalXML(reader io.Reader, isObject bool) (*Tags, error) {
	tagging := &Tags{
		TagSet: &tagSet{
			tagMap:   make(map[string]string),
			isObject: isObject,
		},
	}

	if err := xml.NewDecoder(reader).Decode(tagging); err != nil {
		return nil, err
	}

	return tagging, nil
}

// ParseBucketXML decodes XML data of tags in reader specified in
// https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutBucketTagging.html#API_PutBucketTagging_RequestSyntax.
func ParseBucketXML(reader io.Reader) (*Tags, error) {
	return unmarshalXML(reader, false)
}

// ParseObjectXML decodes XML data of tags in reader specified in
// https://docs.aws.amazon.com/AmazonS3/latest/API/API_PutObjectTagging.html#API_PutObjectTagging_RequestSyntax
func ParseObjectXML(reader io.Reader) (*Tags, error) {
	return unmarshalXML(reader, true)
}

// stringsCut slices s around the first instance of sep,
// returning the text before and after sep.
// The found result reports whether sep appears in s.
// If sep does not appear in s, cut returns s, "", false.
func stringsCut(s, sep string) (before, after string, found bool) {
	if i := strings.Index(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}

func (tags *tagSet) parseTags(tgs string) (err error) {
	for tgs != "" {
		var key string
		key, tgs, _ = stringsCut(tgs, "&")
		if key == "" {
			continue
		}
		key, value, _ := stringsCut(key, "=")
		key, err1 := url.QueryUnescape(key)
		if err1 != nil {
			if err == nil {
				err = err1
			}
			continue
		}
		value, err1 = url.QueryUnescape(value)
		if err1 != nil {
			if err == nil {
				err = err1
			}
			continue
		}
		if err = tags.set(key, value, true); err != nil {
			return err
		}
	}
	return err
}

// Parse decodes HTTP query formatted string into tags which is limited by isObject.
// A query formatted string is like "key1=value1&key2=value2".
func Parse(s string, isObject bool) (*Tags, error) {
	tagging := &Tags{
		TagSet: &tagSet{
			tagMap:   make(map[string]string),
			isObject: isObject,
		},
	}

	if err := tagging.TagSet.parseTags(s); err != nil {
		return nil, err
	}

	return tagging, nil
}

// ParseObjectTags decodes HTTP query formatted string into tags. A query formatted string is like "key1=value1&key2=value2".
func ParseObjectTags(s string) (*Tags, error) {
	return Parse(s, true)
}

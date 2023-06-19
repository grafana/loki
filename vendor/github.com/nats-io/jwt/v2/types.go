/*
 * Copyright 2018-2019 The NATS Authors
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package jwt

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"
)

const MaxInfoLength = 8 * 1024

type Info struct {
	Description string `json:"description,omitempty"`
	InfoURL     string `json:"info_url,omitempty"`
}

func (s Info) Validate(vr *ValidationResults) {
	if len(s.Description) > MaxInfoLength {
		vr.AddError("Description is too long")
	}
	if s.InfoURL != "" {
		if len(s.InfoURL) > MaxInfoLength {
			vr.AddError("Info URL is too long")
		}
		u, err := url.Parse(s.InfoURL)
		if err == nil && (u.Hostname() == "" || u.Scheme == "") {
			err = fmt.Errorf("no hostname or scheme")
		}
		if err != nil {
			vr.AddError("error parsing info url: %v", err)
		}
	}
}

// ExportType defines the type of import/export.
type ExportType int

const (
	// Unknown is used if we don't know the type
	Unknown ExportType = iota
	// Stream defines the type field value for a stream "stream"
	Stream
	// Service defines the type field value for a service "service"
	Service
)

func (t ExportType) String() string {
	switch t {
	case Stream:
		return "stream"
	case Service:
		return "service"
	}
	return "unknown"
}

// MarshalJSON marshals the enum as a quoted json string
func (t *ExportType) MarshalJSON() ([]byte, error) {
	switch *t {
	case Stream:
		return []byte("\"stream\""), nil
	case Service:
		return []byte("\"service\""), nil
	}
	return nil, fmt.Errorf("unknown export type")
}

// UnmarshalJSON unmashals a quoted json string to the enum value
func (t *ExportType) UnmarshalJSON(b []byte) error {
	var j string
	err := json.Unmarshal(b, &j)
	if err != nil {
		return err
	}
	switch j {
	case "stream":
		*t = Stream
		return nil
	case "service":
		*t = Service
		return nil
	}
	return fmt.Errorf("unknown export type %q", j)
}

type RenamingSubject Subject

func (s RenamingSubject) Validate(from Subject, vr *ValidationResults) {
	v := Subject(s)
	v.Validate(vr)
	if from == "" {
		vr.AddError("subject cannot be empty")
	}
	if strings.Contains(string(s), " ") {
		vr.AddError("subject %q cannot have spaces", v)
	}
	matchesSuffix := func(s Subject) bool {
		return s == ">" || strings.HasSuffix(string(s), ".>")
	}
	if matchesSuffix(v) != matchesSuffix(from) {
		vr.AddError("both, renaming subject and subject, need to end or not end in >")
	}
	fromCnt := from.countTokenWildcards()
	refCnt := 0
	for _, tk := range strings.Split(string(v), ".") {
		if tk == "*" {
			refCnt++
		}
		if len(tk) < 2 {
			continue
		}
		if tk[0] == '$' {
			if idx, err := strconv.Atoi(tk[1:]); err == nil {
				if idx > fromCnt {
					vr.AddError("Reference $%d in %q reference * in %q that do not exist", idx, s, from)
				} else {
					refCnt++
				}
			}
		}
	}
	if refCnt != fromCnt {
		vr.AddError("subject does not contain enough * or reference wildcards $[0-9]")
	}
}

// Replaces reference tokens with *
func (s RenamingSubject) ToSubject() Subject {
	if !strings.Contains(string(s), "$") {
		return Subject(s)
	}
	bldr := strings.Builder{}
	tokens := strings.Split(string(s), ".")
	for i, tk := range tokens {
		convert := false
		if len(tk) > 1 && tk[0] == '$' {
			if _, err := strconv.Atoi(tk[1:]); err == nil {
				convert = true
			}
		}
		if convert {
			bldr.WriteString("*")
		} else {
			bldr.WriteString(tk)
		}
		if i != len(tokens)-1 {
			bldr.WriteString(".")
		}
	}
	return Subject(bldr.String())
}

// Subject is a string that represents a NATS subject
type Subject string

// Validate checks that a subject string is valid, ie not empty and without spaces
func (s Subject) Validate(vr *ValidationResults) {
	v := string(s)
	if v == "" {
		vr.AddError("subject cannot be empty")
	}
	if strings.Contains(v, " ") {
		vr.AddError("subject %q cannot have spaces", v)
	}
}

func (s Subject) countTokenWildcards() int {
	v := string(s)
	if v == "*" {
		return 1
	}
	cnt := 0
	for _, t := range strings.Split(v, ".") {
		if t == "*" {
			cnt++
		}
	}
	return cnt
}

// HasWildCards is used to check if a subject contains a > or *
func (s Subject) HasWildCards() bool {
	v := string(s)
	return strings.HasSuffix(v, ".>") ||
		strings.Contains(v, ".*.") ||
		strings.HasSuffix(v, ".*") ||
		strings.HasPrefix(v, "*.") ||
		v == "*" ||
		v == ">"
}

// IsContainedIn does a simple test to see if the subject is contained in another subject
func (s Subject) IsContainedIn(other Subject) bool {
	otherArray := strings.Split(string(other), ".")
	myArray := strings.Split(string(s), ".")

	if len(myArray) > len(otherArray) && otherArray[len(otherArray)-1] != ">" {
		return false
	}

	if len(myArray) < len(otherArray) {
		return false
	}

	for ind, tok := range otherArray {
		myTok := myArray[ind]

		if ind == len(otherArray)-1 && tok == ">" {
			return true
		}

		if tok != myTok && tok != "*" {
			return false
		}
	}

	return true
}

// TimeRange is used to represent a start and end time
type TimeRange struct {
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
}

// Validate checks the values in a time range struct
func (tr *TimeRange) Validate(vr *ValidationResults) {
	format := "15:04:05"

	if tr.Start == "" {
		vr.AddError("time ranges start must contain a start")
	} else {
		_, err := time.Parse(format, tr.Start)
		if err != nil {
			vr.AddError("start in time range is invalid %q", tr.Start)
		}
	}

	if tr.End == "" {
		vr.AddError("time ranges end must contain an end")
	} else {
		_, err := time.Parse(format, tr.End)
		if err != nil {
			vr.AddError("end in time range is invalid %q", tr.End)
		}
	}
}

// Src is a comma separated list of CIDR specifications
type UserLimits struct {
	Src    CIDRList    `json:"src,omitempty"`
	Times  []TimeRange `json:"times,omitempty"`
	Locale string      `json:"times_location,omitempty"`
}

func (u *UserLimits) Empty() bool {
	return reflect.DeepEqual(*u, UserLimits{})
}

func (u *UserLimits) IsUnlimited() bool {
	return len(u.Src) == 0 && len(u.Times) == 0
}

// Limits are used to control acccess for users and importing accounts
type Limits struct {
	UserLimits
	NatsLimits
}

func (l *Limits) IsUnlimited() bool {
	return l.UserLimits.IsUnlimited() && l.NatsLimits.IsUnlimited()
}

// Validate checks the values in a limit struct
func (l *Limits) Validate(vr *ValidationResults) {
	if len(l.Src) != 0 {
		for _, cidr := range l.Src {
			_, ipNet, err := net.ParseCIDR(cidr)
			if err != nil || ipNet == nil {
				vr.AddError("invalid cidr %q in user src limits", cidr)
			}
		}
	}

	if l.Times != nil && len(l.Times) > 0 {
		for _, t := range l.Times {
			t.Validate(vr)
		}
	}

	if l.Locale != "" {
		if _, err := time.LoadLocation(l.Locale); err != nil {
			vr.AddError("could not parse iana time zone by name: %v", err)
		}
	}
}

// Permission defines allow/deny subjects
type Permission struct {
	Allow StringList `json:"allow,omitempty"`
	Deny  StringList `json:"deny,omitempty"`
}

func (p *Permission) Empty() bool {
	return len(p.Allow) == 0 && len(p.Deny) == 0
}

func checkPermission(vr *ValidationResults, subj string, permitQueue bool) {
	tk := strings.Split(subj, " ")
	switch len(tk) {
	case 1:
		Subject(tk[0]).Validate(vr)
	case 2:
		Subject(tk[0]).Validate(vr)
		Subject(tk[1]).Validate(vr)
		if !permitQueue {
			vr.AddError(`Permission Subject "%s" is not allowed to contain queue`, subj)
		}
	default:
		vr.AddError(`Permission Subject "%s" contains too many spaces`, subj)
	}
}

// Validate the allow, deny elements of a permission
func (p *Permission) Validate(vr *ValidationResults, permitQueue bool) {
	for _, subj := range p.Allow {
		checkPermission(vr, subj, permitQueue)
	}
	for _, subj := range p.Deny {
		checkPermission(vr, subj, permitQueue)
	}
}

// ResponsePermission can be used to allow responses to any reply subject
// that is received on a valid subscription.
type ResponsePermission struct {
	MaxMsgs int           `json:"max"`
	Expires time.Duration `json:"ttl"`
}

// Validate the response permission.
func (p *ResponsePermission) Validate(_ *ValidationResults) {
	// Any values can be valid for now.
}

// Permissions are used to restrict subject access, either on a user or for everyone on a server by default
type Permissions struct {
	Pub  Permission          `json:"pub,omitempty"`
	Sub  Permission          `json:"sub,omitempty"`
	Resp *ResponsePermission `json:"resp,omitempty"`
}

// Validate the pub and sub fields in the permissions list
func (p *Permissions) Validate(vr *ValidationResults) {
	if p.Resp != nil {
		p.Resp.Validate(vr)
	}
	p.Sub.Validate(vr, true)
	p.Pub.Validate(vr, false)
}

// StringList is a wrapper for an array of strings
type StringList []string

// Contains returns true if the list contains the string
func (u *StringList) Contains(p string) bool {
	for _, t := range *u {
		if t == p {
			return true
		}
	}
	return false
}

// Add appends 1 or more strings to a list
func (u *StringList) Add(p ...string) {
	for _, v := range p {
		if !u.Contains(v) && v != "" {
			*u = append(*u, v)
		}
	}
}

// Remove removes 1 or more strings from a list
func (u *StringList) Remove(p ...string) {
	for _, v := range p {
		for i, t := range *u {
			if t == v {
				a := *u
				*u = append(a[:i], a[i+1:]...)
				break
			}
		}
	}
}

// TagList is a unique array of lower case strings
// All tag list methods lower case the strings in the arguments
type TagList []string

// Contains returns true if the list contains the tags
func (u *TagList) Contains(p string) bool {
	p = strings.ToLower(strings.TrimSpace(p))
	for _, t := range *u {
		if t == p {
			return true
		}
	}
	return false
}

// Add appends 1 or more tags to a list
func (u *TagList) Add(p ...string) {
	for _, v := range p {
		v = strings.ToLower(strings.TrimSpace(v))
		if !u.Contains(v) && v != "" {
			*u = append(*u, v)
		}
	}
}

// Remove removes 1 or more tags from a list
func (u *TagList) Remove(p ...string) {
	for _, v := range p {
		v = strings.ToLower(strings.TrimSpace(v))
		for i, t := range *u {
			if t == v {
				a := *u
				*u = append(a[:i], a[i+1:]...)
				break
			}
		}
	}
}

type CIDRList TagList

func (c *CIDRList) Contains(p string) bool {
	return (*TagList)(c).Contains(p)
}

func (c *CIDRList) Add(p ...string) {
	(*TagList)(c).Add(p...)
}

func (c *CIDRList) Remove(p ...string) {
	(*TagList)(c).Remove(p...)
}

func (c *CIDRList) Set(values string) {
	*c = CIDRList{}
	c.Add(strings.Split(strings.ToLower(values), ",")...)
}

func (c *CIDRList) UnmarshalJSON(body []byte) (err error) {
	// parse either as array of strings or comma separate list
	var request []string
	var list string
	if err := json.Unmarshal(body, &request); err == nil {
		*c = request
		return nil
	} else if err := json.Unmarshal(body, &list); err == nil {
		c.Set(list)
		return nil
	} else {
		return err
	}
}

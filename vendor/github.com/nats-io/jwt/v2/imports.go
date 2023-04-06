/*
 * Copyright 2018-2020 The NATS Authors
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

// Import describes a mapping from another account into this one
type Import struct {
	Name string `json:"name,omitempty"`
	// Subject field in an import is always from the perspective of the
	// initial publisher - in the case of a stream it is the account owning
	// the stream (the exporter), and in the case of a service it is the
	// account making the request (the importer).
	Subject Subject `json:"subject,omitempty"`
	Account string  `json:"account,omitempty"`
	Token   string  `json:"token,omitempty"`
	// Deprecated: use LocalSubject instead
	// To field in an import is always from the perspective of the subscriber
	// in the case of a stream it is the client of the stream (the importer),
	// from the perspective of a service, it is the subscription waiting for
	// requests (the exporter). If the field is empty, it will default to the
	// value in the Subject field.
	To Subject `json:"to,omitempty"`
	// Local subject used to subscribe (for streams) and publish (for services) to.
	// This value only needs setting if you want to change the value of Subject.
	// If the value of Subject ends in > then LocalSubject needs to end in > as well.
	// LocalSubject can contain $<number> wildcard references where number references the nth wildcard in Subject.
	// The sum of wildcard reference and * tokens needs to match the number of * token in Subject.
	LocalSubject RenamingSubject `json:"local_subject,omitempty"`
	Type         ExportType      `json:"type,omitempty"`
	Share        bool            `json:"share,omitempty"`
}

// IsService returns true if the import is of type service
func (i *Import) IsService() bool {
	return i.Type == Service
}

// IsStream returns true if the import is of type stream
func (i *Import) IsStream() bool {
	return i.Type == Stream
}

// Returns the value of To without triggering the deprecation warning for a read
func (i *Import) GetTo() string {
	return string(i.To)
}

// Validate checks if an import is valid for the wrapping account
func (i *Import) Validate(actPubKey string, vr *ValidationResults) {
	if i == nil {
		vr.AddError("null import is not allowed")
		return
	}
	if !i.IsService() && !i.IsStream() {
		vr.AddError("invalid import type: %q", i.Type)
	}

	if i.Account == "" {
		vr.AddError("account to import from is not specified")
	}

	if i.GetTo() != "" {
		vr.AddWarning("the field to has been deprecated (use LocalSubject instead)")
	}

	i.Subject.Validate(vr)
	if i.LocalSubject != "" {
		i.LocalSubject.Validate(i.Subject, vr)
		if i.To != "" {
			vr.AddError("Local Subject replaces To")
		}
	}

	if i.Share && !i.IsService() {
		vr.AddError("sharing information (for latency tracking) is only valid for services: %q", i.Subject)
	}
	var act *ActivationClaims

	if i.Token != "" {
		var err error
		act, err = DecodeActivationClaims(i.Token)
		if err != nil {
			vr.AddError("import %q contains an invalid activation token", i.Subject)
		}
	}

	if act != nil {
		if !(act.Issuer == i.Account || act.IssuerAccount == i.Account) {
			vr.AddError("activation token doesn't match account for import %q", i.Subject)
		}
		if act.ClaimsData.Subject != actPubKey {
			vr.AddError("activation token doesn't match account it is being included in, %q", i.Subject)
		}
		if act.ImportType != i.Type {
			vr.AddError("mismatch between token import type %s and type of import %s", act.ImportType, i.Type)
		}
		act.validateWithTimeChecks(vr, false)
		subj := i.Subject
		if i.IsService() && i.To != "" {
			subj = i.To
		}
		if !subj.IsContainedIn(act.ImportSubject) {
			vr.AddError("activation token import subject %q doesn't match import %q", act.ImportSubject, i.Subject)
		}
	}
}

// Imports is a list of import structs
type Imports []*Import

// Validate checks if an import is valid for the wrapping account
func (i *Imports) Validate(acctPubKey string, vr *ValidationResults) {
	toSet := make(map[Subject]struct{}, len(*i))
	for _, v := range *i {
		if v == nil {
			vr.AddError("null import is not allowed")
			continue
		}
		if v.Type == Service {
			sub := v.To
			if sub == "" {
				sub = v.LocalSubject.ToSubject()
			}
			if sub == "" {
				sub = v.Subject
			}
			for k := range toSet {
				if sub.IsContainedIn(k) || k.IsContainedIn(sub) {
					vr.AddError("overlapping subject namespace for %q and %q", sub, k)
				}
			}
			if _, ok := toSet[sub]; ok {
				vr.AddError("overlapping subject namespace for %q", v.To)
			}
			toSet[sub] = struct{}{}
		}
		v.Validate(acctPubKey, vr)
	}
}

// Add is a simple way to add imports
func (i *Imports) Add(a ...*Import) {
	*i = append(*i, a...)
}

func (i Imports) Len() int {
	return len(i)
}

func (i Imports) Swap(j, k int) {
	i[j], i[k] = i[k], i[j]
}

func (i Imports) Less(j, k int) bool {
	return i[j].Subject < i[k].Subject
}

/*
Copyright 2015 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package bigtable

import (
	"fmt"
	"strings"
	"time"

	bttdpb "cloud.google.com/go/bigtable/admin/apiv2/adminpb"
	"google.golang.org/protobuf/types/known/durationpb"
)

// PolicyType represents the type of GCPolicy
type PolicyType int

const (
	// PolicyUnspecified represents type of NoGCPolicy
	PolicyUnspecified PolicyType = iota
	// PolicyMaxAge represents type of MaxAgeGCPolicy
	PolicyMaxAge
	// PolicyMaxVersion represents type of MaxVersionGCPolicy
	PolicyMaxVersion
	// PolicyUnion represents type of UnionGCPolicy
	PolicyUnion
	// PolicyIntersection represents type of IntersectionGCPolicy
	PolicyIntersection
)

// A GCPolicy represents a rule that determines which cells are eligible for garbage collection.
type GCPolicy interface {
	String() string
	proto() *bttdpb.GcRule
}

// GetPolicyType takes a gc policy and return appropriate PolicyType
func GetPolicyType(gc GCPolicy) PolicyType {
	switch gc.proto().Rule.(type) {
	case *bttdpb.GcRule_Intersection_:
		return PolicyIntersection
	case *bttdpb.GcRule_Union_:
		return PolicyUnion
	case *bttdpb.GcRule_MaxAge:
		return PolicyMaxAge
	case *bttdpb.GcRule_MaxNumVersions:
		return PolicyMaxVersion
	default:
		return PolicyUnspecified
	}
}

// IntersectionPolicy returns a GC policy that only applies when all its sub-policies apply.
func IntersectionPolicy(sub ...GCPolicy) GCPolicy { return IntersectionGCPolicy{sub} }

// IntersectionGCPolicy with list of children
type IntersectionGCPolicy struct {
	// List of children policy in the intersection
	Children []GCPolicy
}

func (ip IntersectionGCPolicy) String() string {
	var ss []string
	for _, sp := range ip.Children {
		ss = append(ss, sp.String())
	}
	return "(" + strings.Join(ss, " && ") + ")"
}

func (ip IntersectionGCPolicy) proto() *bttdpb.GcRule {
	inter := &bttdpb.GcRule_Intersection{}
	for _, sp := range ip.Children {
		inter.Rules = append(inter.Rules, sp.proto())
	}
	return &bttdpb.GcRule{
		Rule: &bttdpb.GcRule_Intersection_{Intersection: inter},
	}
}

// UnionPolicy returns a GC policy that applies when any of its sub-policies apply.
func UnionPolicy(sub ...GCPolicy) GCPolicy { return UnionGCPolicy{sub} }

// UnionGCPolicy with list of children
type UnionGCPolicy struct {
	// List of children policy in the union
	Children []GCPolicy
}

func (up UnionGCPolicy) String() string {
	var ss []string
	for _, sp := range up.Children {
		ss = append(ss, sp.String())
	}
	return "(" + strings.Join(ss, " || ") + ")"
}

func (up UnionGCPolicy) proto() *bttdpb.GcRule {
	union := &bttdpb.GcRule_Union{}
	for _, sp := range up.Children {
		union.Rules = append(union.Rules, sp.proto())
	}
	return &bttdpb.GcRule{
		Rule: &bttdpb.GcRule_Union_{Union: union},
	}
}

// MaxVersionsPolicy returns a GC policy that applies to all versions of a cell
// except for the most recent n.
func MaxVersionsPolicy(n int) GCPolicy { return MaxVersionsGCPolicy(n) }

// MaxVersionsGCPolicy type alias
type MaxVersionsGCPolicy int

func (mvp MaxVersionsGCPolicy) String() string {
	return fmt.Sprintf("versions() > %d", int(mvp))
}

func (mvp MaxVersionsGCPolicy) proto() *bttdpb.GcRule {
	return &bttdpb.GcRule{Rule: &bttdpb.GcRule_MaxNumVersions{MaxNumVersions: int32(mvp)}}
}

// MaxAgePolicy returns a GC policy that applies to all cells
// older than the given age.
func MaxAgePolicy(d time.Duration) GCPolicy { return MaxAgeGCPolicy(d) }

// MaxAgeGCPolicy type alias
type MaxAgeGCPolicy time.Duration

var units = []struct {
	d      time.Duration
	suffix string
}{
	{24 * time.Hour, "d"},
	{time.Hour, "h"},
	{time.Minute, "m"},
}

func (ma MaxAgeGCPolicy) String() string {
	return fmt.Sprintf("age() > %s", ma.GetDurationString())
}

// GetDurationString returns the duration string of the MaxAgeGCPolicy
func (ma MaxAgeGCPolicy) GetDurationString() string {
	d := time.Duration(ma)
	for _, u := range units {
		if d%u.d == 0 {
			return fmt.Sprintf("%d%s", d/u.d, u.suffix)
		}
	}
	return fmt.Sprintf("%d", d/time.Microsecond)
}

func (ma MaxAgeGCPolicy) proto() *bttdpb.GcRule {
	// This doesn't handle overflows, etc.
	// Fix this if people care about GC policies over 290 years.
	ns := time.Duration(ma).Nanoseconds()
	return &bttdpb.GcRule{
		Rule: &bttdpb.GcRule_MaxAge{MaxAge: &durationpb.Duration{
			Seconds: ns / 1e9,
			Nanos:   int32(ns % 1e9),
		}},
	}
}

type noGCPolicy struct{}

func (n noGCPolicy) String() string { return "" }

func (n noGCPolicy) proto() *bttdpb.GcRule { return &bttdpb.GcRule{Rule: nil} }

// NoGcPolicy applies to all cells setting maxage and maxversions to nil implies no gc policies
func NoGcPolicy() GCPolicy { return noGCPolicy{} }

// GCRuleToString converts the given GcRule proto to a user-visible string.
func GCRuleToString(rule *bttdpb.GcRule) string {
	if rule == nil {
		return "<never>"
	}
	switch r := rule.Rule.(type) {
	case *bttdpb.GcRule_MaxNumVersions:
		return MaxVersionsPolicy(int(r.MaxNumVersions)).String()
	case *bttdpb.GcRule_MaxAge:
		return MaxAgePolicy(time.Duration(r.MaxAge.Seconds) * time.Second).String()
	case *bttdpb.GcRule_Intersection_:
		return joinRules(r.Intersection.Rules, " && ")
	case *bttdpb.GcRule_Union_:
		return joinRules(r.Union.Rules, " || ")
	default:
		return ""
	}
}

func gcRuleToPolicy(rule *bttdpb.GcRule) GCPolicy {
	if rule == nil {
		return NoGcPolicy()
	}
	switch r := rule.Rule.(type) {
	case *bttdpb.GcRule_Intersection_:
		return compoundRuleToPolicy(r.Intersection.Rules, PolicyIntersection)
	case *bttdpb.GcRule_Union_:
		return compoundRuleToPolicy(r.Union.Rules, PolicyUnion)
	case *bttdpb.GcRule_MaxAge:
		return MaxAgePolicy(time.Duration(r.MaxAge.Seconds) * time.Second)
	case *bttdpb.GcRule_MaxNumVersions:
		return MaxVersionsPolicy(int(r.MaxNumVersions))
	default:
		return NoGcPolicy()
	}
}

func joinRules(rules []*bttdpb.GcRule, sep string) string {
	var chunks []string
	for _, r := range rules {
		chunks = append(chunks, GCRuleToString(r))
	}
	return "(" + strings.Join(chunks, sep) + ")"
}

func compoundRuleToPolicy(rules []*bttdpb.GcRule, mode PolicyType) GCPolicy {
	sub := []GCPolicy{}
	for _, r := range rules {
		p := gcRuleToPolicy(r)
		if p.String() != "" {
			sub = append(sub, gcRuleToPolicy(r))
		}
	}

	switch mode {
	case PolicyUnion:
		return UnionGCPolicy{Children: sub}
	case PolicyIntersection:
		return IntersectionGCPolicy{Children: sub}
	default:
		return NoGcPolicy()
	}
}

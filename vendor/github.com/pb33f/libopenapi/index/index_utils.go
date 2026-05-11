// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package index

import (
	"strings"
	"sync"
)

func isHttpMethod(val string) bool {
	switch strings.ToLower(val) {
	case methodTypes[0]:
		return true
	case methodTypes[1]:
		return true
	case methodTypes[2]:
		return true
	case methodTypes[3]:
		return true
	case methodTypes[4]:
		return true
	case methodTypes[5]:
		return true
	case methodTypes[6]:
		return true
	}
	return false
}

func boostrapIndexCollections(index *SpecIndex) {
	index.allRefs = make(map[string]*Reference)
	index.allMappedRefs = make(map[string]*Reference)
	index.refsByLine = make(map[string]map[int]bool)
	index.linesWithRefs = make(map[int]bool)
	index.pathRefs = make(map[string]map[string]*Reference)
	index.paramOpRefs = make(map[string]map[string]map[string][]*Reference)
	index.operationTagsRefs = make(map[string]map[string][]*Reference)
	index.operationDescriptionRefs = make(map[string]map[string]*Reference)
	index.operationSummaryRefs = make(map[string]map[string]*Reference)
	index.paramCompRefs = make(map[string]*Reference)
	index.paramAllRefs = make(map[string]*Reference)
	index.paramInlineDuplicateNames = make(map[string][]*Reference)
	index.globalTagRefs = make(map[string]*Reference)
	index.securitySchemeRefs = make(map[string]*Reference)
	index.requestBodiesRefs = make(map[string]*Reference)
	index.responsesRefs = make(map[string]*Reference)
	index.headersRefs = make(map[string]*Reference)
	index.examplesRefs = make(map[string]*Reference)
	index.callbacksRefs = make(map[string]map[string][]*Reference)
	index.linksRefs = make(map[string]map[string][]*Reference)
	index.callbackRefs = make(map[string]*Reference)
	index.externalSpecIndex = make(map[string]*SpecIndex)
	index.allComponentSchemaDefinitions = &sync.Map{}
	index.allParameters = make(map[string]*Reference)
	index.allSecuritySchemes = &sync.Map{}
	index.allRequestBodies = make(map[string]*Reference)
	index.allResponses = make(map[string]*Reference)
	index.allHeaders = make(map[string]*Reference)
	index.allExamples = make(map[string]*Reference)
	index.allLinks = make(map[string]*Reference)
	index.allCallbacks = make(map[string]*Reference)
	index.allExternalDocuments = make(map[string]*Reference)
	index.securityRequirementRefs = make(map[string]map[string][]*Reference)
	index.polymorphicRefs = make(map[string]*Reference)
	index.refsWithSiblings = make(map[string]Reference)
	index.opServersRefs = make(map[string]map[string][]*Reference)
	index.componentIndexChan = make(chan struct{})
	index.polyComponentIndexChan = make(chan struct{})
	index.allComponentPathItems = make(map[string]*Reference)
}

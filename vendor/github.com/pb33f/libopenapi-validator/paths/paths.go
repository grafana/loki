// Copyright 2023 Princess B33f Heavy Industries / Dave Shanley
// SPDX-License-Identifier: MIT

package paths

import (
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pb33f/libopenapi/orderedmap"

	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/pb33f/libopenapi-validator/config"
	"github.com/pb33f/libopenapi-validator/errors"
	"github.com/pb33f/libopenapi-validator/helpers"
)

// FindPath will find the path in the document that matches the request path. If a successful match was found, then
// the first return value will be a pointer to the PathItem. The second return value will contain any validation errors
// that were picked up when locating the path.
// The third return value will be the path that was found in the document, as it pertains to the contract, so all path
// parameters will not have been replaced with their values from the request - allowing model lookups.
//
// Path matching follows the OpenAPI specification: literal (concrete) paths take precedence over
// parameterized paths, regardless of definition order in the specification.
func FindPath(request *http.Request, document *v3.Document, regexCache config.RegexCache) (*v3.PathItem, []*errors.ValidationError, string) {
	basePaths := getBasePaths(document)
	stripped := StripRequestPath(request, document)

	reqPathSegments := strings.Split(stripped, "/")
	if reqPathSegments[0] == "" {
		reqPathSegments = reqPathSegments[1:]
	}

	candidates := make([]pathCandidate, 0, document.Paths.PathItems.Len())

	for pair := orderedmap.First(document.Paths.PathItems); pair != nil; pair = pair.Next() {
		path := pair.Key()
		pathItem := pair.Value()

		pathForMatching := normalizePathForMatching(path, stripped)

		segs := strings.Split(pathForMatching, "/")
		if segs[0] == "" {
			segs = segs[1:]
		}

		ok := comparePaths(segs, reqPathSegments, basePaths, regexCache)
		if !ok {
			continue
		}

		// Compute specificity score and check if method exists
		score := computeSpecificityScore(path)
		hasMethod := pathHasMethod(pathItem, request.Method)

		candidates = append(candidates, pathCandidate{
			pathItem:  pathItem,
			path:      path,
			score:     score,
			hasMethod: hasMethod,
		})
	}

	if len(candidates) == 0 {
		validationErrors := []*errors.ValidationError{
			{
				ValidationType:    helpers.PathValidation,
				ValidationSubType: helpers.ValidationMissing,
				Message:           fmt.Sprintf("%s Path '%s' not found", request.Method, request.URL.Path),
				Reason: fmt.Sprintf("The %s request contains a path of '%s' "+
					"however that path, or the %s method for that path does not exist in the specification",
					request.Method, request.URL.Path, request.Method),
				SpecLine: -1,
				SpecCol:  -1,
				HowToFix: errors.HowToFixPath,
			},
		}
		errors.PopulateValidationErrors(validationErrors, request, "")
		return nil, validationErrors, ""
	}

	bestWithMethod, bestOverall := selectMatches(candidates)

	if bestWithMethod != nil {
		return bestWithMethod.pathItem, nil, bestWithMethod.path
	}

	// path matches exist but none have the required method
	validationErrors := []*errors.ValidationError{{
		ValidationType:    helpers.PathValidation,
		ValidationSubType: helpers.ValidationMissingOperation,
		Message:           fmt.Sprintf("%s Path '%s' not found", request.Method, request.URL.Path),
		Reason: fmt.Sprintf("The %s method for that path does not exist in the specification",
			request.Method),
		SpecLine: -1,
		SpecCol:  -1,
		HowToFix: errors.HowToFixPath,
	}}
	errors.PopulateValidationErrors(validationErrors, request, bestOverall.path)
	return bestOverall.pathItem, validationErrors, bestOverall.path
}

// normalizePathForMatching removes the fragment from a path template unless
// the request path itself contains a fragment.
func normalizePathForMatching(path, requestPath string) string {
	if strings.Contains(requestPath, "#") {
		return path
	}
	if idx := strings.IndexByte(path, '#'); idx >= 0 {
		return path[:idx]
	}
	return path
}

func getBasePaths(document *v3.Document) []string {
	// extract base path from document to check against paths.
	var basePaths []string
	for _, s := range document.Servers {
		u, err := url.Parse(s.URL)
		// if the host contains special characters, we should attempt to split and parse only the relative path
		if err != nil {
			// split at first occurrence
			_, serverPath, _ := strings.Cut(strings.Replace(s.URL, "//", "", 1), "/")

			if !strings.HasPrefix(serverPath, "/") {
				serverPath = "/" + serverPath
			}

			u, _ = url.Parse(serverPath)
		}

		if u != nil && u.Path != "" {
			basePaths = append(basePaths, u.Path)
		}
	}

	return basePaths
}

// StripRequestPath strips the base path from the request path, based on the server paths provided in the specification
func StripRequestPath(request *http.Request, document *v3.Document) string {
	basePaths := getBasePaths(document)

	// strip any base path
	stripped := stripBaseFromPath(request.URL.EscapedPath(), basePaths)
	if request.URL.Fragment != "" {
		stripped = fmt.Sprintf("%s#%s", stripped, request.URL.Fragment)
	}
	if len(stripped) > 0 && !strings.HasPrefix(stripped, "/") {
		stripped = "/" + stripped
	}
	return stripped
}

func checkPathAgainstBase(docPath, urlPath string, basePaths []string) bool {
	if docPath == urlPath {
		return true
	}
	for _, basePath := range basePaths {
		if basePath[len(basePath)-1] == '/' {
			basePath = basePath[:len(basePath)-1]
		}
		merged := fmt.Sprintf("%s%s", basePath, urlPath)
		if docPath == merged {
			return true
		}
	}
	return false
}

func stripBaseFromPath(path string, basePaths []string) string {
	for i := range basePaths {
		if strings.HasPrefix(path, basePaths[i]) {
			return path[len(basePaths[i]):]
		}
	}
	return path
}

func comparePaths(mapped, requested, basePaths []string, regexCache config.RegexCache) bool {
	if len(mapped) != len(requested) {
		return false // short circuit out
	}
	var imploded []string
	for i, seg := range mapped {
		s := seg
		var rgx *regexp.Regexp

		if regexCache != nil {
			if cachedRegex, found := regexCache.Load(s); found {
				rgx = cachedRegex.(*regexp.Regexp)
			}
		}

		if rgx == nil {
			r, err := helpers.GetRegexForPath(seg)
			if err != nil {
				return false
			}

			rgx = r

			if regexCache != nil {
				regexCache.Store(seg, r)
			}
		}

		if rgx.MatchString(requested[i]) {
			s = requested[i]
		}
		imploded = append(imploded, s)
	}
	l := filepath.Join(imploded...)
	r := filepath.Join(requested...)
	return checkPathAgainstBase(l, r, basePaths)
}

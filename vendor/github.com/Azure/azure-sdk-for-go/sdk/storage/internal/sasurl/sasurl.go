// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License. See License.txt in the project root for license information.

package sasurl

import "strings"

// Append appends the encoded SAS query string to resourceURL. resourceURL may already carry a
// query string (e.g. "snapshot", "versionid", "sharesnapshot", or custom parameters), in which
// case the SAS is merged into it with "&" instead of introducing a second "?". A fragment, if
// present, is kept after the query.
func Append(resourceURL, sasQuery string) string {
	if sasQuery == "" {
		return resourceURL
	}
	base, fragment := cutFragment(resourceURL)
	separator := "?"
	if strings.Contains(base, "?") {
		separator = "&"
	}
	return base + separator + sasQuery + fragment
}

// AppendToAccountURL is like Append, but first adds a trailing slash to the account URL's path
// (to be consistent with the portal). Any existing query string and fragment are split off
// beforehand so the slash lands on the path and not on a query value or fragment.
func AppendToAccountURL(accountURL, sasQuery string) string {
	base, fragment := cutFragment(accountURL)
	path, rawQuery, hasQuery := strings.Cut(base, "?")
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}
	if hasQuery {
		path += "?" + rawQuery
	}
	return Append(path, sasQuery) + fragment
}

// cutFragment splits rawURL at the first "#", returning the fragment with its leading "#"
// (or "" if there is none).
func cutFragment(rawURL string) (base, fragment string) {
	if i := strings.IndexByte(rawURL, '#'); i >= 0 {
		return rawURL[:i], rawURL[i:]
	}
	return rawURL, ""
}

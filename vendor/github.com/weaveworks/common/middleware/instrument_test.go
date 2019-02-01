package middleware_test

import (
	"testing"

	"github.com/weaveworks/common/middleware"
)

func TestMakeLabelValue(t *testing.T) {
	for input, want := range map[string]string{
		"/":                      "root", // special case
		"//":                     "root", // unintended consequence of special case
		"a":                      "a",
		"/foo":                   "foo",
		"foo/":                   "foo",
		"/foo/":                  "foo",
		"/foo/bar":               "foo_bar",
		"foo/bar/":               "foo_bar",
		"/foo/bar/":              "foo_bar",
		"/foo/{orgName}/Bar":     "foo_orgname_bar",
		"/foo/{org_name}/Bar":    "foo_org_name_bar",
		"/foo/{org__name}/Bar":   "foo_org_name_bar",
		"/foo/{org___name}/_Bar": "foo_org_name_bar",
		"/foo.bar/baz.qux/":      "foo_bar_baz_qux",
	} {
		if have := middleware.MakeLabelValue(input); want != have {
			t.Errorf("%q: want %q, have %q", input, want, have)
		}
	}
}

package push

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestExtractAndValidateTrustedLabels(t *testing.T) {
	for _, tc := range []struct {
		name    string
		headers []string
		want    labels.Labels
		wantOK  bool
		wantErr bool
	}{
		{name: "no header"},
		{name: "empty header", headers: []string{""}},
		{name: "empty label set", headers: []string{"{}"}},
		{
			name:    "single label",
			headers: []string{`{username="userA"}`},
			want:    labels.FromStrings("username", "userA"),
			wantOK:  true,
		},
		{
			name:    "multiple labels",
			headers: []string{`{username="userA", team="team-a"}`},
			want:    labels.FromStrings("team", "team-a", "username", "userA"),
			wantOK:  true,
		},
		{name: "malformed value", headers: []string{"username=userA"}, wantErr: true},
		{name: "not a label set", headers: []string{"garbage"}, wantErr: true},
		{name: "reserved internal label", headers: []string{`{__name__="x"}`}, wantErr: true},
		{name: "reserved backfill label", headers: []string{`{__backfill__="x"}`}, wantErr: true},
		{name: "repeated header", headers: []string{`{a="1"}`, `{b="2"}`}, wantErr: true},
		{name: "too long", headers: []string{`{a="` + strings.Repeat("x", maxTrustedLabelsLen) + `"}`}, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", nil)
			for _, h := range tc.headers {
				r.Header.Add(HTTPHeaderTrustedLabelsKey, h)
			}

			got, ok, err := ExtractAndValidateTrustedLabels(r)
			if tc.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), HTTPHeaderTrustedLabelsKey)
				require.False(t, ok)
				require.True(t, got.IsEmpty())
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				require.Equal(t, tc.want.String(), got.String())
			} else {
				require.True(t, got.IsEmpty())
			}
		})
	}
}

func TestExtractAndValidateTrustedLabels_ErrorDoesNotLeakValue(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/loki/api/v1/push", nil)
	r.Header.Set(HTTPHeaderTrustedLabelsKey, `{secret="s3cr3t-token"}=bad`)

	_, _, err := ExtractAndValidateTrustedLabels(r)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "s3cr3t-token")
}

func TestTrustedLabelsContextRoundTrip(t *testing.T) {
	// No value set.
	require.True(t, ExtractTrustedLabelsContext(context.Background()).IsEmpty())

	lbs := labels.FromStrings("username", "userA")
	ctx := InjectTrustedLabelsContext(context.Background(), lbs)
	require.Equal(t, lbs.String(), ExtractTrustedLabelsContext(ctx).String())
}

func TestApplyTrustedLabels(t *testing.T) {
	base := labels.FromStrings("app", "api", "username", "spoofed")

	// Empty trusted labels leave the base untouched.
	require.Equal(t, base.String(), applyTrustedLabels(base, labels.EmptyLabels()).String())

	// Trusted labels override a body label of the same name and add new ones.
	trusted := labels.FromStrings("username", "userA", "team", "team-a")
	got := applyTrustedLabels(base, trusted)
	require.Equal(t, "userA", got.Get("username"))
	require.Equal(t, "team-a", got.Get("team"))
	require.Equal(t, "api", got.Get("app"))
}

func TestApplyTrustedLabelsToSet(t *testing.T) {
	ls := model.LabelSet{"app": "api", "username": "spoofed"}
	applyTrustedLabelsToSet(ls, labels.FromStrings("username", "userA", "team", "team-a"))

	require.Equal(t, model.LabelValue("userA"), ls["username"])
	require.Equal(t, model.LabelValue("team-a"), ls["team"])
	require.Equal(t, model.LabelValue("api"), ls["app"])
}

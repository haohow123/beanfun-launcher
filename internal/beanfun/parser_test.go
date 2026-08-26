package beanfun

import "testing"

func TestSessionKeyFromURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		want string
		ok   bool
	}{
		{"capital pSKey", "https://tw.newlogin.beanfun.com/login/id-pass.aspx?service=999999_T0&pSKey=ABCDEF123", "ABCDEF123", true},
		{"stops at ampersand", "https://host/path?pSKey=TOKEN&next=foo", "TOKEN", true},
		{"lowercase skey", "?skey=LOW", "LOW", true},
		{"pKey without middle S", "?pKey=PK", "PK", true},
		{"absent returns false", "https://host/no-key-here", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := sessionKeyFromURL(tt.url)
			if ok != tt.ok {
				t.Errorf("ok = %v, want %v", ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractVerificationToken(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		html string
		want string
	}{
		{"happy path", `<input name="__RequestVerificationToken" type="hidden" value="TKN123" />`, "TKN123"},
		// Reordered attributes don't match — the regex requires name
		// before value. Production responses always put name first so
		// this is a non-issue in practice.
		{"attributes reordered (no match)", `<input value="TKN456" name="__RequestVerificationToken" type="hidden" />`, ""},
		{"missing input", `<html><body></body></html>`, ""},
		{"different token", `<input name="__RequestVerificationToken" type="hidden" value="abc/def+ghi=" />`, "abc/def+ghi="},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractVerificationToken(tt.html)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeDeeplink(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"wrapped", "https://play.games.gamania.com/foo/deeplink/?url=https%3A%2F%2Fapp.example%2Fauth%3Ft%3D1", "https://app.example/auth?t=1"},
		{"plain passes through", "https://app.example/auth?token=1", "https://app.example/auth?token=1"},
		{"different host passes through", "https://other.com/foo?url=x", "https://other.com/foo?url=x"},
		{"no deeplink in path", "https://play.games.gamania.com/foo/bar/?url=x", "https://play.games.gamania.com/foo/bar/?url=x"},
		{"missing url param", "https://play.games.gamania.com/foo/deeplink/", "https://play.games.gamania.com/foo/deeplink/"},
		{"empty url param", "https://play.games.gamania.com/foo/deeplink/?url=", "https://play.games.gamania.com/foo/deeplink/?url="},
		{"whitespace input", "  ", "  "},
		{"non-absolute URL", "not-a-url", "not-a-url"},
		{"uppercase host", "https://Play.Games.Gamania.com/foo/deeplink/?url=https%3A%2F%2Finner", "https://inner"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := normalizeDeeplink(tt.in)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractLaunchHandoff(t *testing.T) {
	t.Parallel()
	const page = `<script>
	var m_objData = {
		"region": "TW;Production",
		"sn": "b1e2d3c4-0000-1111-2222-33445566aabb",
		"data": "8abcdefghij"
	};
	function LaunchGame() { parent.GGM.SmartLaunch(m_objData); }
	</script>`

	tests := []struct {
		name      string
		body      string
		miss      handoffMiss
		wantCause bool
		region    string
		sn        string
		data      string
	}{
		{
			name:   "full literal",
			body:   page,
			miss:   handoffMissNone,
			region: "TW;Production",
			sn:     "b1e2d3c4-0000-1111-2222-33445566aabb",
			data:   "8abcdefghij",
		},
		{name: "absent", body: `<script>var other = 1;</script>`, miss: handoffMissAbsent},
		// Token on the page but not in the `var x = {…}` shape the regex wants. Distinguishing this
		// from "absent" is the point: the blob may still be there, so it must never reach a preview.
		{
			name: "present but unmatched",
			body: `<script>window.m_objData = buildData();</script>`,
			miss: handoffMissUnmatched,
		},
		// A nested literal makes the regex capture an unbalanced fragment, so it degrades to
		// malformed-json rather than unmatched. Also preview-free, so the gate still holds.
		{
			name:      "nested literal",
			body:      `var m_objData = {"region": "TW", "extra": {"k": 1}, "sn": "abc", "data": "8abc"};`,
			miss:      handoffMissMalformed,
			wantCause: true,
		},
		{name: "not valid JSON", body: `var m_objData = {"region": "TW", oops};`, miss: handoffMissMalformed, wantCause: true},
		{name: "missing sn", body: `var m_objData = {"region": "TW", "data": "8abc"};`, miss: handoffMissEmpty},
		{name: "missing data", body: `var m_objData = {"region": "TW", "sn": "abc"};`, miss: handoffMissEmpty},
		{name: "empty data", body: `var m_objData = {"region": "TW", "sn": "abc", "data": ""};`, miss: handoffMissEmpty},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, miss, cause := extractLaunchHandoff(tt.body)
			if miss != tt.miss {
				t.Fatalf("miss = %q, want %q", miss, tt.miss)
			}
			// The root cause is the point: a parse failure that reports only a category is what made
			// the original log line untraceable.
			if (cause != nil) != tt.wantCause {
				t.Errorf("cause = %v, want non-nil = %v", cause, tt.wantCause)
			}
			if tt.miss != handoffMissNone {
				return
			}
			if got.Region != tt.region {
				t.Errorf("Region = %q, want %q", got.Region, tt.region)
			}
			if got.SN != tt.sn {
				t.Errorf("SN = %q, want %q", got.SN, tt.sn)
			}
			if got.Data != tt.data {
				t.Errorf("Data = %q, want %q", got.Data, tt.data)
			}
		})
	}
}

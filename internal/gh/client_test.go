package gh

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientRefusesCrossOriginPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Link", `<https://evil.example.com/next>; rel="next"`)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	_, err := c.ListReviewComments("o", "r", 1)
	if err == nil {
		t.Fatal("cross-origin Link URL was followed")
	}
	if !strings.Contains(err.Error(), "outside the API origin") {
		t.Errorf("error does not name the origin restriction: %v", err)
	}
}

// The list endpoints must decode positions the way GitHub serves them:
// review comments carry path/line/start_line (line null once outdated,
// start_line null for single-line comments), issue comments carry
// neither. Nulls and absent fields must both land as zero, never error.
func TestClientDecodesCommentPositions(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[
			{"body":"live multi-line","user":{"login":"github-actions[bot]"},"path":"a.go","line":26,"start_line":25},
			{"body":"live single-line","user":{"login":"github-actions[bot]"},"path":"a.go","line":7,"start_line":null},
			{"body":"outdated","user":{"login":"github-actions[bot]"},"path":"a.go","line":null,"start_line":null},
			{"body":"issue comment","user":{"login":"github-actions[bot]"}}
		]`))
	}))
	defer srv.Close()

	got, err := NewClient(srv.URL, "test-token").ListReviewComments("o", "r", 1)
	if err != nil {
		t.Fatalf("ListReviewComments: %v", err)
	}
	want := []struct {
		path        string
		line, start int
	}{
		{"a.go", 26, 25},
		{"a.go", 7, 0},
		{"a.go", 0, 0},
		{"", 0, 0},
	}
	if len(got) != len(want) {
		t.Fatalf("decoded %d comments, want %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Path != w.path || got[i].Line != w.line || got[i].StartLine != w.start {
			t.Errorf("comment %d = {Path:%q Line:%d StartLine:%d}, want {%q %d %d}",
				i, got[i].Path, got[i].Line, got[i].StartLine, w.path, w.line, w.start)
		}
	}
}

func TestClientRejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("x"), maxResponseBytes+1))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	_, err := c.ListReviewComments("o", "r", 1)
	if err == nil {
		t.Fatal("oversized response was accepted")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error does not name the size limit: %v", err)
	}
}

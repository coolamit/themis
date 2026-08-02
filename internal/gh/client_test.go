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
	_, err := c.ListReviewCommentBodies("o", "r", 1)
	if err == nil {
		t.Fatal("cross-origin Link URL was followed")
	}
	if !strings.Contains(err.Error(), "outside the API origin") {
		t.Errorf("error does not name the origin restriction: %v", err)
	}
}

func TestClientRejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("x"), maxResponseBytes+1))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token")
	_, err := c.ListReviewCommentBodies("o", "r", 1)
	if err == nil {
		t.Fatal("oversized response was accepted")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error does not name the size limit: %v", err)
	}
}

package requestid

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

var ulidPattern = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)

func TestNew_LooksLikeAULID(t *testing.T) {
	id := New()
	if !ulidPattern.MatchString(id) {
		t.Fatalf("New() = %q, want a 26-character Crockford-base32 string", id)
	}
}

func TestNew_IsUnique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := New()
		if seen[id] {
			t.Fatalf("New() produced a duplicate: %q", id)
		}
		seen[id] = true
	}
}

func TestMiddleware_GeneratesIDWhenAbsent(t *testing.T) {
	var gotFromContext string
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFromContext = FromContext(r.Context())
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	handler.ServeHTTP(rec, req)

	if gotFromContext == "" {
		t.Fatal("FromContext returned empty string inside the handler")
	}
	if header := rec.Header().Get(HeaderName); header != gotFromContext {
		t.Fatalf("response header %q = %q, want it to match context value %q", HeaderName, header, gotFromContext)
	}
}

func TestMiddleware_ReusesInboundHeader(t *testing.T) {
	const inbound = "01ARZ3NDEKTSV4RRFFQ69G5FAV"

	var gotFromContext string
	handler := Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotFromContext = FromContext(r.Context())
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set(HeaderName, inbound)
	handler.ServeHTTP(rec, req)

	if gotFromContext != inbound {
		t.Fatalf("FromContext() = %q, want the inbound header value %q to be reused", gotFromContext, inbound)
	}
	if header := rec.Header().Get(HeaderName); header != inbound {
		t.Fatalf("response header = %q, want %q echoed back", header, inbound)
	}
}

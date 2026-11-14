package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// aliasGuard must refuse every non-IPP path on the 631 listener: the
// alias is data-plane only — /admin and /pair live on the primary port
// alone. The IPP router itself is permissive (it falls back to the
// default printer for unknown paths), so the guard is the only thing
// keeping the management surface off the well-known port.
func TestAliasGuardBlocksNonIPPPaths(t *testing.T) {
	var reached bool
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	h := aliasGuard(inner)
	for _, path := range []string{"/admin", "/admin/api/status", "/pair", "/", "/favicon.ico"} {
		reached = false
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if reached {
			t.Errorf("path %q reached the inner handler", path)
		}
		if rec.Code != http.StatusNotFound {
			t.Errorf("path %q: code=%d, want 404", path, rec.Code)
		}
	}
}

func TestAliasGuardAllowsIPPPaths(t *testing.T) {
	var got string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Path
		w.WriteHeader(http.StatusOK)
	})
	h := aliasGuard(inner)
	for _, path := range []string{"/ipp/print", "/ipp/print/laser", "/p/abc123/ipp/print/laser"} {
		got = ""
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader("")))
		if got != path {
			t.Errorf("path %q did not reach the router (got %q, code=%d)", path, got, rec.Code)
		}
	}
}
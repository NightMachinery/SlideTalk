package httpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthzReturnsOK(t *testing.T) {
	server := New(ServerOptions{})
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("expected content type application/json, got %q", got)
	}
	if got := response.Body.String(); got != "{\"ok\":true}\n" {
		t.Fatalf("expected health body, got %q", got)
	}
}

func TestUnknownAPIRouteReturnsProblemJSON(t *testing.T) {
	server := New(ServerOptions{})
	request := httptest.NewRequest(http.MethodGet, "/api/missing", nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d", http.StatusNotFound, response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("expected problem JSON content type, got %q", got)
	}
	if got := response.Body.String(); got != "{\"type\":\"about:blank\",\"title\":\"Not Found\",\"status\":404,\"detail\":\"No API route matches /api/missing.\"}\n" {
		t.Fatalf("expected problem body, got %q", got)
	}
}

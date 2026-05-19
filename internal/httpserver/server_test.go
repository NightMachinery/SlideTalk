package httpserver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/NightMachinery/SlideTalk/internal/auth"
	"github.com/NightMachinery/SlideTalk/internal/rooms"
	"github.com/NightMachinery/SlideTalk/internal/store"
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

func TestMeCreatesAndUpdatesUser(t *testing.T) {
	server := newAPITestServer(t)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, apiRequest(http.MethodGet, "/api/me", "", "token-one"))
	if response.Code != http.StatusOK {
		t.Fatalf("get me status = %d body = %s", response.Code, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"displayName":""`)) {
		t.Fatalf("expected empty display name, got %s", response.Body.String())
	}

	response = httptest.NewRecorder()
	server.ServeHTTP(response, apiRequest(http.MethodPatch, "/api/me", `{"displayName":" Ada "}`, "token-one"))
	if response.Code != http.StatusOK {
		t.Fatalf("patch me status = %d body = %s", response.Code, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"displayName":"Ada"`)) {
		t.Fatalf("expected trimmed display name, got %s", response.Body.String())
	}
}

func TestAdminTokenEndpointPromotesUser(t *testing.T) {
	dataDir := t.TempDir()
	server := newAPITestServerWithDataDir(t, dataDir)
	token, err := os.ReadFile(filepath.Join(dataDir, "admin_token"))
	if err != nil {
		t.Fatalf("read admin token: %v", err)
	}

	response := httptest.NewRecorder()
	server.ServeHTTP(response, apiRequest(http.MethodPost, "/api/me/admin-token", `{"token":"`+string(token)+`"}`, "admin-candidate"))
	if response.Code != http.StatusOK {
		t.Fatalf("admin token status = %d body = %s", response.Code, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"isAdmin":true`)) {
		t.Fatalf("expected admin user, got %s", response.Body.String())
	}
}

func TestRoomCreateAndPasswordJoin(t *testing.T) {
	server := newAPITestServer(t)
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Ada"}`, "creator"), http.StatusOK)
	create := serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms", `{"title":"Private","password":"secret"}`, "creator"), http.StatusCreated)
	roomID := extractRoomID(t, create.Body.String())

	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Grace"}`, "guest"), http.StatusOK)
	serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/join", `{}`, "guest"), http.StatusUnauthorized)
	serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/join", `{"password":"secret"}`, "guest"), http.StatusOK)
}

func newAPITestServer(t *testing.T) http.Handler {
	t.Helper()
	return newAPITestServerWithDataDir(t, t.TempDir())
}

func newAPITestServerWithDataDir(t *testing.T, dataDir string) http.Handler {
	t.Helper()
	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := store.Migrate(context.Background(), db.DB); err != nil {
		t.Fatalf("migrate db: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close db: %v", err)
		}
	})
	authService := auth.NewService(db, dataDir)
	if err := authService.EnsureAdminToken(context.Background()); err != nil {
		t.Fatalf("ensure admin token: %v", err)
	}
	return New(ServerOptions{
		AuthService: authService,
		RoomService: rooms.NewService(db),
	})
}

func apiRequest(method string, path string, body string, token string) *http.Request {
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	request := httptest.NewRequest(method, path, reader)
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	return request
}

func serveJSON(t *testing.T, handler http.Handler, request *http.Request, wantStatus int) *httptest.ResponseRecorder {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d, body = %s", request.Method, request.URL.Path, response.Code, wantStatus, response.Body.String())
	}
	return response
}

func extractRoomID(t *testing.T, body string) string {
	t.Helper()
	const marker = `"room":{"id":"`
	index := bytes.Index([]byte(body), []byte(marker))
	if index < 0 {
		t.Fatalf("could not find id in %s", body)
	}
	rest := body[index+len(marker):]
	end := bytes.IndexByte([]byte(rest), '"')
	if end < 0 {
		t.Fatalf("could not find id terminator in %s", body)
	}
	return rest[:end]
}

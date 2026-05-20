package httpserver

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/NightMachinery/SlideTalk/internal/auth"
	"github.com/NightMachinery/SlideTalk/internal/realtime"
	"github.com/NightMachinery/SlideTalk/internal/rooms"
	"github.com/NightMachinery/SlideTalk/internal/slides"
	"github.com/NightMachinery/SlideTalk/internal/store"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
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

func TestWSTicketEndpointAndSocketSnapshot(t *testing.T) {
	server := httptest.NewServer(newAPITestServer(t))
	defer server.Close()

	mustHTTP(t, http.MethodPatch, server.URL+"/api/me", `{"displayName":"Ada"}`, "creator", http.StatusOK)
	createBody := mustHTTP(t, http.MethodPost, server.URL+"/api/rooms", `{"title":"Live","password":""}`, "creator", http.StatusCreated)
	roomID := extractRoomID(t, createBody)
	ticketBody := mustHTTP(t, http.MethodPost, server.URL+"/api/rooms/"+roomID+"/ws-ticket", `{}`, "creator", http.StatusOK)
	var ticket realtime.WSTicket
	if err := json.Unmarshal([]byte(ticketBody), &ticket); err != nil {
		t.Fatalf("decode ticket: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+server.URL[len("http"):]+"/api/ws?ticket="+ticket.Ticket, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	var event realtime.Event
	if err := wsjson.Read(ctx, conn, &event); err != nil {
		t.Fatalf("read snapshot: %v", err)
	}
	if event.Type != realtime.EventSnapshot || event.RoomID != roomID {
		t.Fatalf("unexpected event: %+v", event)
	}

	_, _, err = websocket.Dial(ctx, "ws"+server.URL[len("http"):]+"/api/ws?ticket="+ticket.Ticket, nil)
	if err == nil {
		t.Fatal("reused websocket ticket connected")
	}
}

func TestSlideUploadRejectsNonAdmin(t *testing.T) {
	server := newAPITestServer(t)
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Ada"}`, "creator"), http.StatusOK)
	create := serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms", `{"title":"Decks","password":""}`, "creator"), http.StatusCreated)
	roomID := extractRoomID(t, create.Body.String())
	body, contentType := slideUploadBody(t, roomID, "deck.pdf", []byte("%PDF-1.7\n"), sha256HexTest([]byte("%PDF-1.7\n")))
	request := httptest.NewRequest(http.MethodPost, "/api/slides", body)
	request.Header.Set("Authorization", "Bearer creator")
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden, got %d body = %s", response.Code, response.Body.String())
	}
}

func TestSlideUploadRejectsNonPDFExtension(t *testing.T) {
	server := newAPITestServer(t)
	roomID := createAdminRoom(t, server)
	body, contentType := slideUploadBody(t, roomID, "deck.txt", []byte("%PDF-1.7\n"), sha256HexTest([]byte("%PDF-1.7\n")))
	request := httptest.NewRequest(http.MethodPost, "/api/slides", body)
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d body = %s", response.Code, response.Body.String())
	}
}

func TestSlideUploadRejectsHashMismatch(t *testing.T) {
	server := newAPITestServer(t)
	roomID := createAdminRoom(t, server)
	body, contentType := slideUploadBody(t, roomID, "deck.pdf", []byte("%PDF-1.7\n"), sha256HexTest([]byte("other")))
	request := httptest.NewRequest(http.MethodPost, "/api/slides", body)
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("Content-Type", contentType)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request, got %d body = %s", response.Code, response.Body.String())
	}
}

func TestSlideStatusReportsMissingPhysicalFile(t *testing.T) {
	dataDir := t.TempDir()
	server := newAPITestServerWithDataDir(t, dataDir)
	roomID := createAdminRoom(t, server)
	content := []byte("%PDF-1.7\nmissing\n")
	sum := sha256HexTest(content)
	body, contentType := slideUploadBody(t, roomID, "deck.pdf", content, sum)
	request := httptest.NewRequest(http.MethodPost, "/api/slides", body)
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("Content-Type", contentType)
	serveJSON(t, server, request, http.StatusCreated)
	if err := os.Remove(filepath.Join(dataDir, "slides", sum+".pdf")); err != nil {
		t.Fatalf("remove slide: %v", err)
	}

	response := httptest.NewRecorder()
	server.ServeHTTP(response, apiRequest(http.MethodGet, "/api/slides/"+sum, "", "admin"))

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d body = %s", response.Code, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"missing":true`)) {
		t.Fatalf("expected missing status, got %s", response.Body.String())
	}
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
	return dataDirHandler{Handler: New(ServerOptions{
		AuthService:  authService,
		RoomService:  rooms.NewService(db),
		Hub:          realtime.NewHub(db, authService, rooms.NewService(db)),
		SlideService: mustSlideService(t, db, dataDir),
	}), dataDir: dataDir}
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

func mustHTTP(t *testing.T, method string, url string, body string, token string, wantStatus int) string {
	t.Helper()
	request, err := http.NewRequest(method, url, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("http request: %v", err)
	}
	defer response.Body.Close()
	buffer := new(bytes.Buffer)
	if _, err := buffer.ReadFrom(response.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("%s %s status = %d, want %d, body = %s", method, url, response.StatusCode, wantStatus, buffer.String())
	}
	return buffer.String()
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

func createAdminRoom(t *testing.T, server http.Handler) string {
	t.Helper()
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Ada"}`, "admin"), http.StatusOK)
	data, ok := serverDataDir(server)
	if ok {
		token, err := os.ReadFile(filepath.Join(data, "admin_token"))
		if err != nil {
			t.Fatalf("read admin token: %v", err)
		}
		serveJSON(t, server, apiRequest(http.MethodPost, "/api/me/admin-token", `{"token":"`+string(token)+`"}`, "admin"), http.StatusOK)
	} else {
		t.Fatal("test server did not expose data dir")
	}
	create := serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms", `{"title":"Decks","password":""}`, "admin"), http.StatusCreated)
	return extractRoomID(t, create.Body.String())
}

func slideUploadBody(t *testing.T, roomID string, name string, content []byte, sha string) (*bytes.Buffer, string) {
	t.Helper()
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	fields := map[string]string{
		"roomId":       roomID,
		"sha256":       sha,
		"originalName": name,
		"expiresAt":    time.Now().UTC().Add(14 * 24 * time.Hour).Format(time.RFC3339Nano),
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	part, err := writer.CreateFormFile("file", name)
	if err != nil {
		t.Fatalf("create form file: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close writer: %v", err)
	}
	return body, writer.FormDataContentType()
}

func sha256HexTest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

type dataDirHandler struct {
	http.Handler
	dataDir string
}

func serverDataDir(handler http.Handler) (string, bool) {
	value, ok := handler.(dataDirHandler)
	return value.dataDir, ok
}

func mustSlideService(t *testing.T, db *store.DB, dataDir string) *slides.Service {
	t.Helper()
	service, err := slides.NewService(db, filepath.Join(dataDir, "slides"), 200*1024*1024)
	if err != nil {
		t.Fatalf("new slide service: %v", err)
	}
	return service
}

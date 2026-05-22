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
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/NightMachinery/SlideTalk/internal/audio"
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

func TestSecurityHeadersAreApplied(t *testing.T) {
	server := New(ServerOptions{})
	request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if got := response.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := response.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy = %q, want no-referrer", got)
	}
	csp := response.Header().Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'self'", "worker-src 'self' blob:", "connect-src 'self' ws: wss:", "frame-ancestors 'none'"} {
		if !strings.Contains(csp, want) {
			t.Fatalf("Content-Security-Policy = %q, missing %q", csp, want)
		}
	}
	if got := response.Header().Get("Permissions-Policy"); got != "camera=(), microphone=(), geolocation=()" {
		t.Fatalf("Permissions-Policy = %q, want valid camera/microphone/geolocation policy", got)
	}
	if strings.Contains(response.Header().Get("Permissions-Policy"), "browsing-topics") {
		t.Fatalf("Permissions-Policy must not include browsing-topics: %q", response.Header().Get("Permissions-Policy"))
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

func TestJSONRequestBodyLimit(t *testing.T) {
	server := newAPITestServer(t)
	oversizedName := strings.Repeat("A", 1<<20)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"`+oversizedName+`"}`, "token-one"))

	if response.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d", http.StatusBadRequest, response.Code)
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

func TestAdminListAndDemotionRequireAdmin(t *testing.T) {
	server := newAPITestServer(t)
	adminID := createAdminUser(t, server, "admin", "Ada")
	otherID := createAdminUser(t, server, "other-admin", "Grace")

	serveJSON(t, server, apiRequest(http.MethodGet, "/api/admins", "", "guest"), http.StatusForbidden)
	list := serveJSON(t, server, apiRequest(http.MethodGet, "/api/admins", "", "admin"), http.StatusOK)
	if !bytes.Contains(list.Body.Bytes(), []byte(adminID)) || !bytes.Contains(list.Body.Bytes(), []byte(otherID)) {
		t.Fatalf("admin list missing users: %s", list.Body.String())
	}

	serveJSON(t, server, apiRequest(http.MethodDelete, "/api/admins/"+otherID, "", "guest"), http.StatusForbidden)
	serveJSON(t, server, apiRequest(http.MethodDelete, "/api/admins/"+otherID, "", "admin"), http.StatusNoContent)
	updated := serveJSON(t, server, apiRequest(http.MethodGet, "/api/me", "", "other-admin"), http.StatusOK)
	if !bytes.Contains(updated.Body.Bytes(), []byte(`"isAdmin":false`)) {
		t.Fatalf("expected demoted user, got %s", updated.Body.String())
	}
}

func TestDemoteAllProtectsRecoveryPath(t *testing.T) {
	dataDir := t.TempDir()
	server := newAPITestServerWithDataDir(t, dataDir)
	createAdminUser(t, server, "admin", "Ada")
	createAdminUser(t, server, "other-admin", "Grace")
	if err := os.Remove(filepath.Join(dataDir, "admin_token")); err != nil {
		t.Fatalf("remove admin token: %v", err)
	}

	serveJSON(t, server, apiRequest(http.MethodPost, "/api/admins/demote-all", `{"includeSelf":true}`, "admin"), http.StatusBadRequest)
	serveJSON(t, server, apiRequest(http.MethodPost, "/api/admins/demote-all", `{}`, "admin"), http.StatusNoContent)

	admin := serveJSON(t, server, apiRequest(http.MethodGet, "/api/me", "", "admin"), http.StatusOK)
	if !bytes.Contains(admin.Body.Bytes(), []byte(`"isAdmin":true`)) {
		t.Fatalf("caller should remain admin when includeSelf is false: %s", admin.Body.String())
	}
	other := serveJSON(t, server, apiRequest(http.MethodGet, "/api/me", "", "other-admin"), http.StatusOK)
	if !bytes.Contains(other.Body.Bytes(), []byte(`"isAdmin":false`)) {
		t.Fatalf("other admin should be demoted: %s", other.Body.String())
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

func TestRoomCreateAcceptsRoomMode(t *testing.T) {
	server := newAPITestServer(t)
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Ada"}`, "creator"), http.StatusOK)
	create := serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms", `{"title":"Listening","password":"","roomMode":"audio"}`, "creator"), http.StatusCreated)
	roomID := extractRoomID(t, create.Body.String())

	response := serveJSON(t, server, apiRequest(http.MethodGet, "/api/rooms/"+roomID+"/snapshot", "", "creator"), http.StatusOK)

	if !bytes.Contains(response.Body.Bytes(), []byte(`"roomMode":"audio"`)) {
		t.Fatalf("snapshot missing audio room mode: %s", response.Body.String())
	}
}

func TestRoomPasswordAttemptsAreRateLimited(t *testing.T) {
	server := newAPITestServer(t)
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Ada"}`, "creator"), http.StatusOK)
	create := serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms", `{"title":"Private","password":"secret"}`, "creator"), http.StatusCreated)
	roomID := extractRoomID(t, create.Body.String())
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Grace"}`, "guest"), http.StatusOK)

	for range 5 {
		serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/join", `{"password":"wrong"}`, "guest"), http.StatusUnauthorized)
	}
	serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/join", `{"password":"secret"}`, "guest"), http.StatusTooManyRequests)
}

func TestRoomSettingsRequireModAndCanClearPassword(t *testing.T) {
	server := newAPITestServer(t)
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Ada"}`, "mod"), http.StatusOK)
	create := serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms", `{"title":"Private","password":"secret"}`, "mod"), http.StatusCreated)
	roomID := extractRoomID(t, create.Body.String())
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Grace"}`, "guest"), http.StatusOK)
	serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/join", `{"password":"secret"}`, "guest"), http.StatusOK)

	body := `{"title":"Open Room","passwordAction":"clear","roomMode":"markdown","allowParticipantMarkdown":true,"sharedNavigationEnabled":true,"raiseHandMode":"queue"}`
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/rooms/"+roomID+"/settings", body, "guest"), http.StatusForbidden)
	updated := serveJSON(t, server, apiRequest(http.MethodPatch, "/api/rooms/"+roomID+"/settings", body, "mod"), http.StatusOK)
	if !bytes.Contains(updated.Body.Bytes(), []byte(`"title":"Open Room"`)) || !bytes.Contains(updated.Body.Bytes(), []byte(`"hasPassword":false`)) {
		t.Fatalf("settings response did not include renamed open room: %s", updated.Body.String())
	}

	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Lin"}`, "new-guest"), http.StatusOK)
	serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/join", `{}`, "new-guest"), http.StatusOK)
}

func TestMigrationLinkRequiresModeratorAndReturnsBearerSecretOnce(t *testing.T) {
	server := newAPITestServer(t)
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Ada"}`, "mod"), http.StatusOK)
	create := serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms", `{"title":"Private","password":""}`, "mod"), http.StatusCreated)
	roomID := extractRoomID(t, create.Body.String())
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Grace"}`, "guest"), http.StatusOK)
	serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/join", `{}`, "guest"), http.StatusOK)

	serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/migration-link", `{}`, "guest"), http.StatusForbidden)
	response := serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/migration-link", `{}`, "mod"), http.StatusCreated)

	if !bytes.Contains(response.Body.Bytes(), []byte(`"roomId":"`+roomID+`"`)) {
		t.Fatalf("migration link response missing room id: %s", response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte(`"migrationId":"`)) {
		t.Fatalf("migration link response missing bearer secret: %s", response.Body.String())
	}
	if bytes.Contains(response.Body.Bytes(), []byte(`"migrationIdHash"`)) {
		t.Fatalf("migration link response leaked hash: %s", response.Body.String())
	}
}

func TestMigrationLinkCanJoinPasswordRoom(t *testing.T) {
	server := newAPITestServer(t)
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Ada"}`, "mod"), http.StatusOK)
	create := serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms", `{"title":"Private","password":"secret"}`, "mod"), http.StatusCreated)
	roomID := extractRoomID(t, create.Body.String())
	linkResponse := serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/migration-link", `{}`, "mod"), http.StatusCreated)
	var link rooms.MigrationLink
	if err := json.Unmarshal(linkResponse.Body.Bytes(), &link); err != nil {
		t.Fatalf("decode migration link: %v", err)
	}
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Grace"}`, "guest"), http.StatusOK)
	serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/join", `{"migrationId":"wrong"}`, "guest"), http.StatusUnauthorized)
	serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/join", `{"migrationId":"`+link.MigrationID+`"}`, "guest"), http.StatusOK)
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

func TestRoomSnapshotEndpointReturnsInitialLiveState(t *testing.T) {
	server := newAPITestServer(t)
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Ada"}`, "creator"), http.StatusOK)
	create := serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms", `{"title":"Live","password":""}`, "creator"), http.StatusCreated)
	roomID := extractRoomID(t, create.Body.String())

	response := serveJSON(t, server, apiRequest(http.MethodGet, "/api/rooms/"+roomID+"/snapshot", "", "creator"), http.StatusOK)

	var snapshot realtime.Snapshot
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode snapshot: %v", err)
	}
	if snapshot.Room.ID != roomID || snapshot.Room.Title != "Live" {
		t.Fatalf("snapshot room = %+v, want id %q title Live", snapshot.Room, roomID)
	}
	if snapshot.Caller.Role != rooms.RoleMod {
		t.Fatalf("caller role = %q, want %q", snapshot.Caller.Role, rooms.RoleMod)
	}
	if len(snapshot.Participants) != 1 || snapshot.Participants[0].DisplayName != "Ada" {
		t.Fatalf("participants = %+v, want creator participant", snapshot.Participants)
	}
	for _, field := range []string{`"observers":[]`, `"hands":[]`} {
		if !bytes.Contains(response.Body.Bytes(), []byte(field)) {
			t.Fatalf("snapshot response missing %s: %s", field, response.Body.String())
		}
	}
}

func TestRoomSnapshotEndpointRequiresMembership(t *testing.T) {
	server := newAPITestServer(t)
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Ada"}`, "creator"), http.StatusOK)
	create := serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms", `{"title":"Live","password":""}`, "creator"), http.StatusCreated)
	roomID := extractRoomID(t, create.Body.String())
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Grace"}`, "stranger"), http.StatusOK)

	serveJSON(t, server, apiRequest(http.MethodGet, "/api/rooms/"+roomID+"/snapshot", "", "stranger"), http.StatusNotFound)
}

func TestWSTicketCreationIsRateLimited(t *testing.T) {
	server := newAPITestServer(t)
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Ada"}`, "creator"), http.StatusOK)
	create := serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms", `{"title":"Live","password":""}`, "creator"), http.StatusCreated)
	roomID := extractRoomID(t, create.Body.String())

	for range 10 {
		serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/ws-ticket", `{}`, "creator"), http.StatusOK)
	}
	serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/ws-ticket", `{}`, "creator"), http.StatusTooManyRequests)
}

func TestRoomSlideReplacementAndRemovalControlsReferenceOnly(t *testing.T) {
	dataDir := t.TempDir()
	server := newAPITestServerWithDataDir(t, dataDir)
	roomID := createAdminRoom(t, server)
	content := []byte("%PDF-1.7\nfirst\n")
	sum := sha256HexTest(content)
	body, contentType := slideUploadBody(t, roomID, "first.pdf", content, sum)
	request := httptest.NewRequest(http.MethodPost, "/api/rooms/"+roomID+"/slide", body)
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("Content-Type", contentType)
	serveJSON(t, server, request, http.StatusCreated)

	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Grace"}`, "guest"), http.StatusOK)
	serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/join", `{}`, "guest"), http.StatusOK)
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/rooms/"+roomID+"/slide", `{"expiresAt":"`+time.Now().UTC().Add(48*time.Hour).Format(time.RFC3339Nano)+`"}`, "guest"), http.StatusForbidden)

	serveJSON(t, server, apiRequest(http.MethodDelete, "/api/rooms/"+roomID+"/slide", "", "guest"), http.StatusForbidden)
	serveJSON(t, server, apiRequest(http.MethodDelete, "/api/rooms/"+roomID+"/slide", "", "admin"), http.StatusNoContent)
	if _, err := os.Stat(filepath.Join(dataDir, "slides", sum+".pdf")); err != nil {
		t.Fatalf("slide file should remain after room reference removal: %v", err)
	}
	serveJSON(t, server, apiRequest(http.MethodGet, "/api/rooms/"+roomID+"/slide/file", "", "admin"), http.StatusNotFound)
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

func TestRoomMemberCanFetchCurrentPDFAndNonMemberCannot(t *testing.T) {
	server := newAPITestServer(t)
	roomID := createAdminRoom(t, server)
	content := []byte("%PDF-1.7\nstream\n")
	body, contentType := slideUploadBody(t, roomID, "deck.pdf", content, sha256HexTest(content))
	request := httptest.NewRequest(http.MethodPost, "/api/slides", body)
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("Content-Type", contentType)
	serveJSON(t, server, request, http.StatusCreated)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, apiRequest(http.MethodGet, "/api/rooms/"+roomID+"/slide/file", "", "admin"))
	if response.Code != http.StatusOK {
		t.Fatalf("member status = %d body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("content type = %q, want application/pdf", got)
	}
	if !bytes.Equal(response.Body.Bytes(), content) {
		t.Fatalf("streamed content mismatch: %q", response.Body.String())
	}

	response = httptest.NewRecorder()
	server.ServeHTTP(response, apiRequest(http.MethodGet, "/api/rooms/"+roomID+"/slide/file", "", "stranger"))
	if response.Code != http.StatusNotFound {
		t.Fatalf("non-member status = %d body = %s", response.Code, response.Body.String())
	}
}

func TestRoomMemberCanFetchCurrentImageWithStoredContentType(t *testing.T) {
	server := newAPITestServer(t)
	roomID := createAdminRoom(t, server)
	content := []byte("\x89PNG\r\n\x1a\nstream\n")
	body, contentType := slideUploadBody(t, roomID, "diagram.png", content, sha256HexTest(content))
	request := httptest.NewRequest(http.MethodPost, "/api/slides", body)
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("Content-Type", contentType)
	serveJSON(t, server, request, http.StatusCreated)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, apiRequest(http.MethodGet, "/api/rooms/"+roomID+"/slide/file", "", "admin"))
	if response.Code != http.StatusOK {
		t.Fatalf("member status = %d body = %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type = %q, want image/png", got)
	}
	if !bytes.Equal(response.Body.Bytes(), content) {
		t.Fatalf("streamed content mismatch: %q", response.Body.String())
	}
}

func TestRoomSlideFileReportsManualDeletion(t *testing.T) {
	dataDir := t.TempDir()
	server := newAPITestServerWithDataDir(t, dataDir)
	roomID := createAdminRoom(t, server)
	content := []byte("%PDF-1.7\ndeleted\n")
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
	server.ServeHTTP(response, apiRequest(http.MethodGet, "/api/rooms/"+roomID+"/slide/file", "", "admin"))

	if response.Code != http.StatusNotFound {
		t.Fatalf("missing file status = %d body = %s", response.Code, response.Body.String())
	}
	if !bytes.Contains(response.Body.Bytes(), []byte("deleted manually")) {
		t.Fatalf("expected manual deletion problem, got %s", response.Body.String())
	}
}

func TestAudioDownloadIsAvailableToParticipantsByDefault(t *testing.T) {
	dataDir := t.TempDir()
	server := newAPITestServerWithDataDir(t, dataDir)
	roomID := createAdminRoom(t, server)
	content := testWAVBytes()
	body, contentType := audioUploadBody(t, roomID, "track.wav", content, sha256HexTest(content))
	request := httptest.NewRequest(http.MethodPost, "/api/rooms/"+roomID+"/audio", body)
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("Content-Type", contentType)
	upload := serveJSON(t, server, request, http.StatusCreated)
	var status audio.Status
	if err := json.Unmarshal(upload.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode audio status: %v", err)
	}

	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Grace"}`, "participant"), http.StatusOK)
	serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/join", `{}`, "participant"), http.StatusOK)

	serveJSON(t, server, apiRequest(http.MethodGet, "/api/rooms/"+roomID+"/audio/"+status.ID, "", "participant"), http.StatusOK)
	serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/audio/"+status.ID+"/download-link", `{}`, "participant"), http.StatusCreated)

	db, err := store.Open(dataDir)
	if err != nil {
		t.Fatalf("open db to set observer: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Fatalf("close observer db: %v", err)
		}
	})
	if _, err := db.ExecContext(context.Background(), `update room_members set role = ? where room_id = ? and role = ?`, rooms.RoleObserver, roomID, rooms.RoleParticipant); err != nil {
		t.Fatalf("set observer role: %v", err)
	}
	serveJSON(t, server, apiRequest(http.MethodGet, "/api/rooms/"+roomID+"/audio/"+status.ID, "", "participant"), http.StatusOK)
	serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/audio/"+status.ID+"/download-link", `{}`, "participant"), http.StatusCreated)

	body, contentType = audioUploadBody(t, roomID, "observer.wav", content, sha256HexTest(content))
	request = httptest.NewRequest(http.MethodPost, "/api/rooms/"+roomID+"/audio", body)
	request.Header.Set("Authorization", "Bearer participant")
	request.Header.Set("Content-Type", contentType)
	serveJSON(t, server, request, http.StatusForbidden)
}

func TestParticipantAudioUploadRequiresAudienceUploadSetting(t *testing.T) {
	server := newAPITestServer(t)
	roomID := createAdminRoom(t, server)
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"Grace"}`, "participant"), http.StatusOK)
	serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/join", `{}`, "participant"), http.StatusOK)

	content := testWAVBytes()
	body, contentType := audioUploadBody(t, roomID, "participant.wav", content, sha256HexTest(content))
	request := httptest.NewRequest(http.MethodPost, "/api/rooms/"+roomID+"/audio", body)
	request.Header.Set("Authorization", "Bearer participant")
	request.Header.Set("Content-Type", contentType)
	serveJSON(t, server, request, http.StatusForbidden)

	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/rooms/"+roomID+"/settings", `{"allowAudienceAudioUpload":true}`, "admin"), http.StatusOK)

	body, contentType = audioUploadBody(t, roomID, "participant.wav", content, sha256HexTest(content))
	request = httptest.NewRequest(http.MethodPost, "/api/rooms/"+roomID+"/audio", body)
	request.Header.Set("Authorization", "Bearer participant")
	request.Header.Set("Content-Type", contentType)
	serveJSON(t, server, request, http.StatusCreated)
}

func TestAudioDownloadTokenAllowsExternalDownloadWithoutAuth(t *testing.T) {
	server := newAPITestServer(t)
	roomID := createAdminRoom(t, server)
	content := testWAVBytes()
	body, contentType := audioUploadBody(t, roomID, "track.wav", content, sha256HexTest(content))
	request := httptest.NewRequest(http.MethodPost, "/api/rooms/"+roomID+"/audio", body)
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("Content-Type", contentType)
	upload := serveJSON(t, server, request, http.StatusCreated)
	var status audio.Status
	if err := json.Unmarshal(upload.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode audio status: %v", err)
	}

	linkResponse := serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms/"+roomID+"/audio/"+status.ID+"/download-link", `{}`, "admin"), http.StatusCreated)
	var link struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(linkResponse.Body.Bytes(), &link); err != nil {
		t.Fatalf("decode download link: %v", err)
	}
	if strings.Contains(link.URL, "admin") || strings.Contains(link.URL, "user") {
		t.Fatalf("download URL leaks auth-ish identifier: %s", link.URL)
	}

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, link.URL, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("token download status = %d body = %s", response.Code, response.Body.String())
	}
	if !bytes.Equal(response.Body.Bytes(), content) {
		t.Fatalf("token download mismatch")
	}

	serveJSON(t, server, apiRequest(http.MethodDelete, "/api/rooms/"+roomID+"/audio/"+status.ID, "", "admin"), http.StatusNoContent)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(http.MethodGet, link.URL, nil))
	if response.Code != http.StatusForbidden && response.Code != http.StatusNotFound {
		t.Fatalf("removed token status = %d, want forbidden or not found", response.Code)
	}
}

func TestAudioMetadataPatchAllowsModUploaderNameAndUploaderTitle(t *testing.T) {
	server := newAPITestServer(t)
	roomID := createAdminRoom(t, server)
	content := testWAVBytes()
	body, contentType := audioUploadBody(t, roomID, "track.wav", content, sha256HexTest(content))
	request := httptest.NewRequest(http.MethodPost, "/api/rooms/"+roomID+"/audio", body)
	request.Header.Set("Authorization", "Bearer admin")
	request.Header.Set("Content-Type", contentType)
	upload := serveJSON(t, server, request, http.StatusCreated)
	var status audio.Status
	if err := json.Unmarshal(upload.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode audio status: %v", err)
	}

	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/rooms/"+roomID+"/audio/"+status.ID, `{"title":"Renamed","uploaderDisplayName":"Guest Singer"}`, "admin"), http.StatusNoContent)
	snapshot := serveJSON(t, server, apiRequest(http.MethodGet, "/api/rooms/"+roomID+"/snapshot", "", "admin"), http.StatusOK)
	if !bytes.Contains(snapshot.Body.Bytes(), []byte(`"title":"Renamed"`)) || !bytes.Contains(snapshot.Body.Bytes(), []byte(`"uploaderDisplayName":"Guest Singer"`)) {
		t.Fatalf("snapshot missing patched metadata: %s", snapshot.Body.String())
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
		AudioService: mustAudioService(t, db, dataDir),
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
	createAdminUser(t, server, "admin", "Ada")
	create := serveJSON(t, server, apiRequest(http.MethodPost, "/api/rooms", `{"title":"Decks","password":""}`, "admin"), http.StatusCreated)
	return extractRoomID(t, create.Body.String())
}

func createAdminUser(t *testing.T, server http.Handler, token string, displayName string) string {
	t.Helper()
	serveJSON(t, server, apiRequest(http.MethodPatch, "/api/me", `{"displayName":"`+displayName+`"}`, token), http.StatusOK)
	data, ok := serverDataDir(server)
	if ok {
		adminToken, err := os.ReadFile(filepath.Join(data, "admin_token"))
		if err != nil {
			t.Fatalf("read admin token: %v", err)
		}
		serveJSON(t, server, apiRequest(http.MethodPost, "/api/me/admin-token", `{"token":"`+string(adminToken)+`"}`, token), http.StatusOK)
	} else {
		t.Fatal("test server did not expose data dir")
	}
	me := serveJSON(t, server, apiRequest(http.MethodGet, "/api/me", "", token), http.StatusOK)
	var user auth.User
	if err := json.Unmarshal(me.Body.Bytes(), &user); err != nil {
		t.Fatalf("decode admin user: %v", err)
	}
	return user.ID
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

func audioUploadBody(t *testing.T, roomID string, name string, content []byte, sha string) (*bytes.Buffer, string) {
	t.Helper()
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	fields := map[string]string{
		"roomId":       roomID,
		"sha256":       sha,
		"originalName": name,
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatalf("write field: %v", err)
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+name+`"`)
	header.Set("Content-Type", "audio/wav")
	part, err := writer.CreatePart(header)
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

func testWAVBytes() []byte {
	return []byte{
		'R', 'I', 'F', 'F', 40, 0, 0, 0, 'W', 'A', 'V', 'E',
		'f', 'm', 't', ' ', 16, 0, 0, 0, 1, 0, 1, 0,
		64, 31, 0, 0, 128, 62, 0, 0, 1, 0, 8, 0,
		'd', 'a', 't', 'a', 4, 0, 0, 0, 128, 128, 128, 128,
	}
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

func mustAudioService(t *testing.T, db *store.DB, dataDir string) *audio.Service {
	t.Helper()
	service, err := audio.NewService(db, filepath.Join(dataDir, "audio"), 50*1024*1024, 0)
	if err != nil {
		t.Fatalf("new audio service: %v", err)
	}
	return service
}

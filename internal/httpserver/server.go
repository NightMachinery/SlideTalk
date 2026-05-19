// Package httpserver provides SlideTalk's HTTP API and static asset server.
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/NightMachinery/SlideTalk/internal/auth"
	"github.com/NightMachinery/SlideTalk/internal/realtime"
	"github.com/NightMachinery/SlideTalk/internal/rooms"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// ServerOptions configures the SlideTalk HTTP server.
type ServerOptions struct {
	StaticDir   string
	AuthService *auth.Service
	RoomService *rooms.Service
	Hub         *realtime.Hub
}

// New returns a configured HTTP handler for the SlideTalk server.
func New(options ServerOptions) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	app := &appServer{
		auth:         options.AuthService,
		rooms:        options.RoomService,
		hub:          options.Hub,
		adminLimiter: newFailureLimiter(5, 15*time.Minute),
	}

	var staticHandler http.Handler
	if options.StaticDir != "" && dirExists(options.StaticDir) {
		staticHandler = http.FileServer(http.Dir(options.StaticDir))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			mux.ServeHTTP(w, r)
			return
		}

		if isAPIPath(r.URL.Path) {
			app.routeAPI(w, r)
			return
		}

		if staticHandler != nil {
			serveStatic(staticHandler, options.StaticDir, w, r)
			return
		}

		http.NotFound(w, r)
	})
}

type appServer struct {
	auth         *auth.Service
	rooms        *rooms.Service
	hub          *realtime.Hub
	adminLimiter *failureLimiter
}

type contextKey string

const userContextKey contextKey = "user"

func (s *appServer) routeAPI(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/api/me":
		s.withUser(s.getMe)(w, r)
	case r.Method == http.MethodPatch && r.URL.Path == "/api/me":
		s.withUser(s.patchMe)(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/me/admin-token":
		s.withUser(s.postAdminToken)(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/rooms":
		s.withUser(s.postRoom)(w, r)
	case r.Method == http.MethodGet && roomPathValue(r.URL.Path, "") != "":
		s.withUser(s.getRoom)(w, withRoomID(r, roomPathValue(r.URL.Path, "")))
	case r.Method == http.MethodPost && roomPathValue(r.URL.Path, "/join") != "":
		s.withUser(s.joinRoom)(w, withRoomID(r, roomPathValue(r.URL.Path, "/join")))
	case r.Method == http.MethodPost && roomPathValue(r.URL.Path, "/ws-ticket") != "":
		s.withUser(s.postWSTicket)(w, withRoomID(r, roomPathValue(r.URL.Path, "/ws-ticket")))
	case r.Method == http.MethodGet && r.URL.Path == "/api/ws":
		s.handleWS(w, r)
	default:
		writeProblem(w, http.StatusNotFound, "Not Found", "No API route matches "+r.URL.Path+".")
	}
}

func (s *appServer) withUser(next func(http.ResponseWriter, *http.Request, auth.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.auth == nil {
			writeProblem(w, http.StatusNotFound, "Not Found", "No API route matches "+r.URL.Path+".")
			return
		}
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			writeProblem(w, http.StatusUnauthorized, "Unauthorized", "Missing bearer token.")
			return
		}
		user, err := s.auth.EnsureUser(r.Context(), token)
		if err != nil {
			writeProblem(w, http.StatusUnauthorized, "Unauthorized", "Invalid bearer token.")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userContextKey, user)), user)
	}
}

func (s *appServer) getMe(w http.ResponseWriter, _ *http.Request, user auth.User) {
	writeJSON(w, http.StatusOK, user)
}

func (s *appServer) patchMe(w http.ResponseWriter, r *http.Request, user auth.User) {
	var input struct {
		DisplayName string `json:"displayName"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.auth.UpdateDisplayName(r.Context(), user.ID, input.DisplayName); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	updated, err := s.auth.GetUser(r.Context(), user.ID)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Could not read updated profile.")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *appServer) postAdminToken(w http.ResponseWriter, r *http.Request, user auth.User) {
	limitKey := r.RemoteAddr + ":" + user.ID
	if s.adminLimiter.blocked(limitKey) {
		writeProblem(w, http.StatusTooManyRequests, "Too Many Requests", "Too many failed admin token attempts.")
		return
	}
	var input struct {
		Token string `json:"token"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	promoted, err := s.auth.PromoteWithAdminToken(r.Context(), user.ID, input.Token)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Could not verify admin token.")
		return
	}
	if !promoted {
		s.adminLimiter.recordFailure(limitKey)
		writeProblem(w, http.StatusUnauthorized, "Unauthorized", "Invalid admin token.")
		return
	}
	s.adminLimiter.reset(limitKey)
	updated, err := s.auth.GetUser(r.Context(), user.ID)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Could not read updated profile.")
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

func (s *appServer) postRoom(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.rooms == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "No API route matches "+r.URL.Path+".")
		return
	}
	var input struct {
		Title    string `json:"title"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	room, err := s.rooms.Create(r.Context(), user.ID, rooms.CreateInput{Title: input.Title, Password: input.Password})
	if err != nil {
		writeRoomError(w, err)
		return
	}
	details, err := s.rooms.GetForUser(r.Context(), room.ID, user.ID)
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Could not read created room.")
		return
	}
	writeJSON(w, http.StatusCreated, details)
}

func (s *appServer) getRoom(w http.ResponseWriter, r *http.Request, user auth.User) {
	details, err := s.rooms.GetForUser(r.Context(), roomIDFromContext(r.Context()), user.ID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, details)
}

func (s *appServer) joinRoom(w http.ResponseWriter, r *http.Request, user auth.User) {
	var input struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	roomID := roomIDFromContext(r.Context())
	if _, err := s.rooms.Join(r.Context(), roomID, user.ID, rooms.JoinInput{Password: input.Password}); err != nil {
		writeRoomError(w, err)
		return
	}
	details, err := s.rooms.GetForUser(r.Context(), roomID, user.ID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, details)
}

func (s *appServer) postWSTicket(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.hub == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "No API route matches "+r.URL.Path+".")
		return
	}
	ticket, err := s.hub.IssueTicket(r.Context(), roomIDFromContext(r.Context()), user.ID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, ticket)
}

func (s *appServer) handleWS(w http.ResponseWriter, r *http.Request) {
	if s.hub == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "No API route matches "+r.URL.Path+".")
		return
	}
	claims, err := s.hub.ConsumeTicket(r.URL.Query().Get("ticket"))
	if err != nil {
		writeProblem(w, http.StatusUnauthorized, "Unauthorized", "Invalid websocket ticket.")
		return
	}
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	client := &realtime.Client{RoomID: claims.RoomID, UserID: claims.UserID, Send: make(chan realtime.Event, 16)}
	s.hub.Register(client)
	defer s.hub.Unregister(client)
	s.hub.BroadcastSnapshot(r.Context(), claims.RoomID, "")

	errCh := make(chan error, 1)
	go func() {
		for event := range client.Send {
			if err := wsjson.Write(r.Context(), conn, event); err != nil {
				errCh <- err
				return
			}
		}
		errCh <- nil
	}()
	for {
		var command realtime.Command
		readErr := make(chan error, 1)
		go func() {
			readErr <- wsjson.Read(r.Context(), conn, &command)
		}()
		select {
		case err := <-errCh:
			if err != nil {
				return
			}
		case err := <-readErr:
			if err != nil {
				return
			}
		}
		if command.Type == "" {
			return
		}
		if err := s.hub.HandleCommand(r.Context(), claims.RoomID, claims.UserID, command); err != nil {
			client.Send <- realtime.Event{Type: realtime.EventError, RequestID: command.RequestID, Code: codeForRealtimeError(err), Message: messageForRealtimeError(err)}
			continue
		}
		s.hub.BroadcastSnapshot(r.Context(), claims.RoomID, command.RequestID)
	}
}

func writeRoomError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, rooms.ErrInvalidPassword):
		writeProblem(w, http.StatusUnauthorized, "Unauthorized", "Invalid room password.")
	case errors.Is(err, rooms.ErrDisplayNameRequired):
		writeProblem(w, http.StatusBadRequest, "Bad Request", "Set a display name before using rooms.")
	case errors.Is(err, rooms.ErrInvalidTitle):
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
	case errors.Is(err, rooms.ErrNotFound), errors.Is(err, rooms.ErrNotMember):
		writeProblem(w, http.StatusNotFound, "Not Found", "Room was not found.")
	case errors.Is(err, rooms.ErrKicked):
		writeProblem(w, http.StatusForbidden, "Forbidden", "You were removed from this room.")
	default:
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Room operation failed.")
	}
}

func codeForRealtimeError(err error) string {
	switch {
	case errors.Is(err, realtime.ErrForbidden):
		return "forbidden"
	case errors.Is(err, realtime.ErrBadCommand), errors.Is(err, rooms.ErrInvalidRole), errors.Is(err, rooms.ErrInvalidReorder):
		return "bad_request"
	default:
		return "failed"
	}
}

func messageForRealtimeError(err error) string {
	switch {
	case errors.Is(err, realtime.ErrForbidden):
		return "Only moderators can change room order."
	case errors.Is(err, rooms.ErrLastMod):
		return "A room must keep at least one moderator."
	default:
		return err.Error()
	}
}

func healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]bool{"ok": true})
}

func isAPIPath(path string) bool {
	return path == "/api" || len(path) > len("/api/") && path[:len("/api/")] == "/api/"
}

func writeProblem(w http.ResponseWriter, status int, title string, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{
		Type:   "about:blank",
		Title:  title,
		Status: status,
		Detail: detail,
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, destination any) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "Request body must be valid JSON.")
		return false
	}
	return true
}

func bearerToken(header string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	return token, token != ""
}

type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail"`
}

const roomIDContextKey contextKey = "roomID"

func roomPathValue(path string, suffix string) string {
	const prefix = "/api/rooms/"
	if !strings.HasPrefix(path, prefix) || !strings.HasSuffix(path, suffix) {
		return ""
	}
	value := strings.TrimSuffix(strings.TrimPrefix(path, prefix), suffix)
	if value == "" || strings.Contains(value, "/") {
		return ""
	}
	return value
}

func withRoomID(r *http.Request, roomID string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), roomIDContextKey, roomID))
}

func roomIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(roomIDContextKey).(string)
	return value
}

type failureLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	failures map[string]failureState
}

type failureState struct {
	count     int
	firstSeen time.Time
}

func newFailureLimiter(limit int, window time.Duration) *failureLimiter {
	return &failureLimiter{limit: limit, window: window, failures: make(map[string]failureState)}
}

func (l *failureLimiter) blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	state, ok := l.failures[key]
	if !ok {
		return false
	}
	if time.Since(state.firstSeen) > l.window {
		delete(l.failures, key)
		return false
	}
	return state.count >= l.limit
}

func (l *failureLimiter) recordFailure(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	state, ok := l.failures[key]
	if !ok || time.Since(state.firstSeen) > l.window {
		l.failures[key] = failureState{count: 1, firstSeen: time.Now()}
		return
	}
	state.count++
	l.failures[key] = state
}

func (l *failureLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}

func serveStatic(staticHandler http.Handler, staticDir string, w http.ResponseWriter, r *http.Request) {
	requested := filepath.Clean(r.URL.Path)
	if requested == "." || requested == "/" {
		requested = "index.html"
	}

	fullPath := filepath.Join(staticDir, requested)
	if _, err := os.Stat(fullPath); errors.Is(err, os.ErrNotExist) {
		r = r.Clone(r.Context())
		r.URL.Path = "/"
	}

	staticHandler.ServeHTTP(w, r)
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

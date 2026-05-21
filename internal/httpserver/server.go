// Package httpserver provides SlideTalk's HTTP API and static asset server.
package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/NightMachinery/SlideTalk/internal/auth"
	"github.com/NightMachinery/SlideTalk/internal/realtime"
	"github.com/NightMachinery/SlideTalk/internal/rooms"
	"github.com/NightMachinery/SlideTalk/internal/slides"
	"nhooyr.io/websocket"
	"nhooyr.io/websocket/wsjson"
)

// ServerOptions configures the SlideTalk HTTP server.
type ServerOptions struct {
	StaticDir    string
	AuthService  *auth.Service
	RoomService  *rooms.Service
	Hub          *realtime.Hub
	SlideService *slides.Service
}

// New returns a configured HTTP handler for the SlideTalk server.
func New(options ServerOptions) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthz)
	app := &appServer{
		auth:         options.AuthService,
		rooms:        options.RoomService,
		hub:          options.Hub,
		slides:       options.SlideService,
		adminLimiter: newFailureLimiter(5, 15*time.Minute),
		joinLimiter:  newFailureLimiter(5, 15*time.Minute),
		wsLimiter:    newFailureLimiter(10, time.Minute),
	}

	var staticHandler http.Handler
	if options.StaticDir != "" && dirExists(options.StaticDir) {
		staticHandler = http.FileServer(http.Dir(options.StaticDir))
	}

	return securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	}))
}

type appServer struct {
	auth         *auth.Service
	rooms        *rooms.Service
	hub          *realtime.Hub
	slides       *slides.Service
	adminLimiter *failureLimiter
	joinLimiter  *failureLimiter
	wsLimiter    *failureLimiter
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
	case r.Method == http.MethodGet && r.URL.Path == "/api/admins":
		s.withUser(s.getAdmins)(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/admins/demote-all":
		s.withUser(s.postDemoteAllAdmins)(w, r)
	case r.Method == http.MethodDelete && adminPathValue(r.URL.Path) != "":
		s.withUser(s.deleteAdmin)(w, withAdminID(r, adminPathValue(r.URL.Path)))
	case r.Method == http.MethodPost && r.URL.Path == "/api/rooms":
		s.withUser(s.postRoom)(w, r)
	case r.Method == http.MethodGet && slidePathValue(r.URL.Path) != "":
		s.withUser(s.getSlideStatus)(w, withSlideSHA(r, slidePathValue(r.URL.Path)))
	case r.Method == http.MethodPost && r.URL.Path == "/api/slides":
		s.withUser(s.postSlide)(w, r)
	case r.Method == http.MethodPatch && roomPathValue(r.URL.Path, "/settings") != "":
		s.withUser(s.patchRoomSettings)(w, withRoomID(r, roomPathValue(r.URL.Path, "/settings")))
	case r.Method == http.MethodPost && roomPathValue(r.URL.Path, "/slide") != "":
		s.withUser(s.postRoomSlide)(w, withRoomID(r, roomPathValue(r.URL.Path, "/slide")))
	case r.Method == http.MethodPatch && roomPathValue(r.URL.Path, "/slide") != "":
		s.withUser(s.patchRoomSlide)(w, withRoomID(r, roomPathValue(r.URL.Path, "/slide")))
	case r.Method == http.MethodDelete && roomPathValue(r.URL.Path, "/slide") != "":
		s.withUser(s.deleteRoomSlide)(w, withRoomID(r, roomPathValue(r.URL.Path, "/slide")))
	case r.Method == http.MethodGet && roomPathValue(r.URL.Path, "/slide/file") != "":
		s.withUser(s.getRoomSlideFile)(w, withRoomID(r, roomPathValue(r.URL.Path, "/slide/file")))
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

func (s *appServer) getAdmins(w http.ResponseWriter, r *http.Request, user auth.User) {
	if !requireAdmin(w, user) {
		return
	}
	admins, err := s.auth.ListAdmins(r.Context())
	if err != nil {
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Could not list admins.")
		return
	}
	writeJSON(w, http.StatusOK, admins)
}

func (s *appServer) deleteAdmin(w http.ResponseWriter, r *http.Request, user auth.User) {
	if !requireAdmin(w, user) {
		return
	}
	if err := s.auth.DemoteAdmin(r.Context(), adminIDFromContext(r.Context())); err != nil {
		writeAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *appServer) postDemoteAllAdmins(w http.ResponseWriter, r *http.Request, user auth.User) {
	if !requireAdmin(w, user) {
		return
	}
	var input struct {
		IncludeSelf bool `json:"includeSelf"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	if err := s.auth.DemoteAllAdmins(r.Context(), user.ID, input.IncludeSelf); err != nil {
		writeAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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
	limitKey := r.RemoteAddr + ":" + user.ID + ":" + roomID
	if s.joinLimiter.blocked(limitKey) {
		writeProblem(w, http.StatusTooManyRequests, "Too Many Requests", "Too many room password attempts.")
		return
	}
	if _, err := s.rooms.Join(r.Context(), roomID, user.ID, rooms.JoinInput{Password: input.Password}); err != nil {
		if errors.Is(err, rooms.ErrInvalidPassword) {
			s.joinLimiter.recordFailure(limitKey)
		}
		writeRoomError(w, err)
		return
	}
	s.joinLimiter.reset(limitKey)
	details, err := s.rooms.GetForUser(r.Context(), roomID, user.ID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, details)
}

func (s *appServer) patchRoomSettings(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.rooms == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "No API route matches "+r.URL.Path+".")
		return
	}
	roomID := roomIDFromContext(r.Context())
	if !s.requireRoomMod(w, r, user, roomID) {
		return
	}
	var input struct {
		Title                    *string `json:"title"`
		Password                 *string `json:"password"`
		PasswordAction           string  `json:"passwordAction"`
		NoSlideMode              *bool   `json:"noSlideMode"`
		AllowParticipantMarkdown *bool   `json:"allowParticipantMarkdown"`
		SharedNavigationEnabled  *bool   `json:"sharedNavigationEnabled"`
		RaiseHandMode            *string `json:"raiseHandMode"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	settings := rooms.SettingsInput{
		Title:                    input.Title,
		NoSlideMode:              input.NoSlideMode,
		AllowParticipantMarkdown: input.AllowParticipantMarkdown,
		SharedNavigationEnabled:  input.SharedNavigationEnabled,
		RaiseHandMode:            input.RaiseHandMode,
	}
	switch strings.TrimSpace(input.PasswordAction) {
	case "":
	case "set":
		if input.Password == nil {
			writeProblem(w, http.StatusBadRequest, "Bad Request", "Password is required when setting a room password.")
			return
		}
		settings.Password = input.Password
	case "clear":
		settings.ClearPassword = true
	default:
		writeProblem(w, http.StatusBadRequest, "Bad Request", "Password action must be set or clear.")
		return
	}
	room, err := s.rooms.UpdateSettings(r.Context(), roomID, settings)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	if s.hub != nil {
		s.hub.BroadcastSnapshot(r.Context(), roomID, "")
	}
	writeJSON(w, http.StatusOK, room)
}

func (s *appServer) postWSTicket(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.hub == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "No API route matches "+r.URL.Path+".")
		return
	}
	roomID := roomIDFromContext(r.Context())
	limitKey := r.RemoteAddr + ":" + user.ID + ":" + roomID
	if s.wsLimiter.blocked(limitKey) {
		writeProblem(w, http.StatusTooManyRequests, "Too Many Requests", "Too many websocket ticket requests.")
		return
	}
	ticket, err := s.hub.IssueTicket(r.Context(), roomID, user.ID)
	if err != nil {
		writeRoomError(w, err)
		return
	}
	s.wsLimiter.recordFailure(limitKey)
	writeJSON(w, http.StatusOK, ticket)
}

func (s *appServer) getSlideStatus(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.slides == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "No API route matches "+r.URL.Path+".")
		return
	}
	if !user.IsAdmin {
		writeProblem(w, http.StatusForbidden, "Forbidden", "Only site admins can inspect slide storage.")
		return
	}
	status, err := s.slides.Status(r.Context(), slideSHAFromContext(r.Context()))
	if err != nil {
		writeSlideError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (s *appServer) postSlide(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.slides == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "No API route matches "+r.URL.Path+".")
		return
	}
	if !user.IsAdmin {
		writeProblem(w, http.StatusForbidden, "Forbidden", "Only site admins can upload slides.")
		return
	}
	status, ok := s.storeSlideFromMultipart(w, r, user.ID, "")
	if !ok {
		return
	}
	if s.hub != nil {
		s.hub.BroadcastSnapshot(r.Context(), status.RoomID, "")
	}
	writeJSON(w, http.StatusCreated, status)
}

func (s *appServer) postRoomSlide(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.slides == nil || s.rooms == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "No API route matches "+r.URL.Path+".")
		return
	}
	roomID := roomIDFromContext(r.Context())
	if !s.requireRoomMod(w, r, user, roomID) {
		return
	}
	status, ok := s.storeSlideFromMultipart(w, r, user.ID, roomID)
	if !ok {
		return
	}
	if s.hub != nil {
		s.hub.BroadcastSnapshot(r.Context(), roomID, "")
	}
	writeJSON(w, http.StatusCreated, status)
}

func (s *appServer) patchRoomSlide(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.slides == nil || s.rooms == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "No API route matches "+r.URL.Path+".")
		return
	}
	roomID := roomIDFromContext(r.Context())
	if !s.requireRoomMod(w, r, user, roomID) {
		return
	}
	if !user.IsAdmin {
		writeProblem(w, http.StatusForbidden, "Forbidden", "Only site admins can change slide expiration.")
		return
	}
	var input struct {
		ExpiresAt string `json:"expiresAt"`
	}
	if !decodeJSON(w, r, &input) {
		return
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(input.ExpiresAt))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "Slide expiration must be an RFC3339 timestamp.")
		return
	}
	if err := s.slides.UpdateRoomExpiration(r.Context(), roomID, expiresAt); err != nil {
		writeSlideError(w, err)
		return
	}
	if s.hub != nil {
		s.hub.BroadcastSnapshot(r.Context(), roomID, "")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *appServer) deleteRoomSlide(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.slides == nil || s.rooms == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "No API route matches "+r.URL.Path+".")
		return
	}
	roomID := roomIDFromContext(r.Context())
	if !s.requireRoomMod(w, r, user, roomID) {
		return
	}
	if err := s.slides.RemoveRoomSlide(r.Context(), roomID); err != nil {
		writeSlideError(w, err)
		return
	}
	if s.hub != nil {
		s.hub.BroadcastSnapshot(r.Context(), roomID, "")
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *appServer) storeSlideFromMultipart(w http.ResponseWriter, r *http.Request, userID string, roomID string) (slides.Status, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, s.slides.MaxBytes()+10<<20)
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "Slide upload must be multipart form data.")
		return slides.Status{}, false
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(r.FormValue("expiresAt")))
	if err != nil {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "Slide expiration must be an RFC3339 timestamp.")
		return slides.Status{}, false
	}
	if roomID == "" {
		roomID = r.FormValue("roomId")
	}
	var file io.Reader
	mimeType := ""
	uploadedFile, header, err := r.FormFile("file")
	if err == nil {
		defer uploadedFile.Close()
		file = uploadedFile
		mimeType = header.Header.Get("Content-Type")
	} else if !errors.Is(err, http.ErrMissingFile) {
		writeProblem(w, http.StatusBadRequest, "Bad Request", "Slide file field is invalid.")
		return slides.Status{}, false
	}
	status, err := s.slides.Store(r.Context(), userID, slides.StoreInput{
		RoomID:       roomID,
		SHA256:       r.FormValue("sha256"),
		OriginalName: r.FormValue("originalName"),
		ExpiresAt:    expiresAt,
		MIMEType:     mimeType,
		File:         file,
	})
	if err != nil {
		writeSlideError(w, err)
		return slides.Status{}, false
	}
	return status, true
}

func (s *appServer) getRoomSlideFile(w http.ResponseWriter, r *http.Request, user auth.User) {
	if s.slides == nil || s.rooms == nil {
		writeProblem(w, http.StatusNotFound, "Not Found", "No API route matches "+r.URL.Path+".")
		return
	}
	roomID := roomIDFromContext(r.Context())
	if _, err := s.rooms.GetForUser(r.Context(), roomID, user.ID); err != nil {
		writeRoomError(w, err)
		return
	}
	file, err := s.slides.CurrentRoomFile(r.Context(), roomID)
	if err != nil {
		writeSlideError(w, err)
		return
	}
	handle, err := os.Open(file.StoredPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeSlideError(w, slides.ErrMissingFile)
			return
		}
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Could not read slide file.")
		return
	}
	defer handle.Close()
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="`+strings.ReplaceAll(file.OriginalName, `"`, "")+`"`)
	http.ServeContent(w, r, file.OriginalName, time.Time{}, handle)
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
			if errors.Is(err, realtime.ErrNoBroadcast) {
				continue
			}
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
	case errors.Is(err, rooms.ErrInvalidRaiseHandMode):
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
	case errors.Is(err, rooms.ErrNotFound), errors.Is(err, rooms.ErrNotMember):
		writeProblem(w, http.StatusNotFound, "Not Found", "Room was not found.")
	case errors.Is(err, rooms.ErrKicked):
		writeProblem(w, http.StatusForbidden, "Forbidden", "You were removed from this room.")
	default:
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Room operation failed.")
	}
}

func writeAdminError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, auth.ErrNotFound):
		writeProblem(w, http.StatusNotFound, "Not Found", "Admin was not found.")
	case errors.Is(err, auth.ErrNoAdminRecovery):
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
	default:
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Admin operation failed.")
	}
}

func requireAdmin(w http.ResponseWriter, user auth.User) bool {
	if user.IsAdmin {
		return true
	}
	writeProblem(w, http.StatusForbidden, "Forbidden", "Only site admins can manage admins.")
	return false
}

func (s *appServer) requireRoomMod(w http.ResponseWriter, r *http.Request, user auth.User, roomID string) bool {
	details, err := s.rooms.GetForUser(r.Context(), roomID, user.ID)
	if err != nil {
		writeRoomError(w, err)
		return false
	}
	if details.Membership.Role != rooms.RoleMod {
		writeProblem(w, http.StatusForbidden, "Forbidden", "Only room moderators can change room settings.")
		return false
	}
	return true
}

func writeSlideError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, slides.ErrUnsupportedFile), errors.Is(err, slides.ErrHashMismatch), errors.Is(err, slides.ErrInvalidHash), errors.Is(err, slides.ErrInvalidExpiry), errors.Is(err, slides.ErrFileRequired), errors.Is(err, slides.ErrTooLarge):
		writeProblem(w, http.StatusBadRequest, "Bad Request", err.Error())
	case errors.Is(err, slides.ErrNoRoomSlide):
		writeProblem(w, http.StatusNotFound, "Not Found", "Room has no slide file.")
	case errors.Is(err, slides.ErrMissingFile):
		writeProblem(w, http.StatusNotFound, "Not Found", err.Error())
	default:
		writeProblem(w, http.StatusInternalServerError, "Internal Server Error", "Slide operation failed.")
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

func securityHeaders(next http.Handler) http.Handler {
	const csp = "default-src 'self'; base-uri 'self'; object-src 'none'; frame-ancestors 'none'; img-src 'self' data: blob:; script-src 'self'; style-src 'self' 'unsafe-inline'; font-src 'self'; worker-src 'self' blob:; connect-src 'self' ws: wss: blob:"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", csp)
		next.ServeHTTP(w, r)
	})
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
const slideSHAContextKey contextKey = "slideSHA"
const adminIDContextKey contextKey = "adminID"

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

func slidePathValue(path string) string {
	const prefix = "/api/slides/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	value := strings.TrimPrefix(path, prefix)
	if value == "" || strings.Contains(value, "/") {
		return ""
	}
	return value
}

func adminPathValue(path string) string {
	const prefix = "/api/admins/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	value := strings.TrimPrefix(path, prefix)
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

func withSlideSHA(r *http.Request, sha string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), slideSHAContextKey, sha))
}

func slideSHAFromContext(ctx context.Context) string {
	value, _ := ctx.Value(slideSHAContextKey).(string)
	return value
}

func withAdminID(r *http.Request, adminID string) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), adminIDContextKey, adminID))
}

func adminIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(adminIDContextKey).(string)
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

// Package audio manages room-scoped shared audio storage and playlists.
package audio

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/NightMachinery/SlideTalk/internal/store"
)

const (
	octetMIME    = "application/octet-stream"
	detectPrefix = 512
)

// Service stores audio files under a configured local directory.
type Service struct {
	db           *store.DB
	audioDir     string
	maxBytes     int64
	minFreeBytes int64
	freeSpace    func(string) (int64, error)
}

// StoreInput is the room audio upload payload.
type StoreInput struct {
	RoomID          string
	SHA256          string
	OriginalName    string
	MIMEType        string
	File            io.Reader
	IsAdmin         bool
	MetadataTitle   string
	DurationSeconds int
	Cover           io.Reader
	CoverMIMEType   string
}

// Status describes a stored room audio track.
type Status struct {
	ID               string `json:"id"`
	SHA256           string `json:"sha256"`
	OriginalName     string `json:"originalName"`
	Title            string `json:"title"`
	MetadataTitle    string `json:"metadataTitle"`
	MIMEType         string `json:"mimeType"`
	SizeBytes        int64  `json:"sizeBytes"`
	DurationSeconds  int    `json:"durationSeconds"`
	HasCover         bool   `json:"hasCover"`
	UploadedByUserID string `json:"uploadedByUserId"`
	AlreadyUploaded  bool   `json:"alreadyUploaded"`
	Missing          bool   `json:"missing"`
}

// RoomFile describes a downloadable room audio track.
type RoomFile struct {
	ID           string
	SHA256       string
	OriginalName string
	StoredPath   string
	MIMEType     string
}

// Track is public room playlist metadata.
type Track struct {
	ID                  string `json:"id"`
	SHA256              string `json:"sha256"`
	OriginalName        string `json:"originalName"`
	Title               string `json:"title"`
	MetadataTitle       string `json:"metadataTitle"`
	MIMEType            string `json:"mimeType"`
	SizeBytes           int64  `json:"sizeBytes"`
	DurationSeconds     int    `json:"durationSeconds"`
	HasCover            bool   `json:"hasCover"`
	UploadedByUserID    string `json:"uploadedByUserId"`
	UploadedByName      string `json:"uploadedByName"`
	UploaderDisplayName string `json:"uploaderDisplayName"`
	DisplayOrder        int    `json:"displayOrder"`
	Missing             bool   `json:"missing"`
}

var (
	ErrUnsupportedFile       = errors.New("only audio files are supported")
	ErrHashMismatch          = errors.New("audio hash does not match uploaded file")
	ErrInvalidHash           = errors.New("audio hash must be a SHA-256 hex digest")
	ErrMissingFile           = errors.New("audio file was deleted manually")
	ErrFileRequired          = errors.New("audio file is required")
	ErrTooLarge              = errors.New("audio file exceeds size limit")
	ErrNoRoomAudio           = errors.New("room has no audio track")
	ErrInsufficientFreeSpace = errors.New("upload would leave too little free disk space")
	ErrNotTrackUploaderOrMod = errors.New("only the uploader or a moderator can remove this audio track")
	ErrInvalidDownloadToken  = errors.New("invalid audio download token")
	ErrInvalidTrackMetadata  = errors.New("audio metadata is invalid")
)

// NewService creates the audio directory and returns a service.
func NewService(db *store.DB, audioDir string, maxBytes int64, minFreeBytes int64) (*Service, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("audio max bytes must be positive")
	}
	if minFreeBytes < 0 {
		return nil, fmt.Errorf("minimum free space must not be negative")
	}
	if err := os.MkdirAll(audioDir, 0o700); err != nil {
		return nil, fmt.Errorf("create audio dir: %w", err)
	}
	return &Service{db: db, audioDir: audioDir, maxBytes: maxBytes, minFreeBytes: minFreeBytes, freeSpace: filesystemFreeSpace}, nil
}

// MaxBytes returns the configured non-admin upload size limit.
func (s *Service) MaxBytes() int64 {
	return s.maxBytes
}

// Store validates and stores audio, then appends it to the room playlist.
func (s *Service) Store(ctx context.Context, userID string, input StoreInput) (Status, error) {
	sha := strings.ToLower(strings.TrimSpace(input.SHA256))
	if !validSHA256(sha) {
		return Status{}, ErrInvalidHash
	}
	media, err := mediaFor(input.OriginalName, input.MIMEType)
	if err != nil {
		return Status{}, err
	}
	if input.File == nil {
		return Status{}, ErrFileRequired
	}
	status, err := s.status(ctx, sha)
	if err != nil {
		return Status{}, err
	}
	if status.Missing {
		return Status{}, ErrMissingFile
	}
	path := s.pathFor(sha, media.ext)
	metadataTitle := trimLimit(input.MetadataTitle, 200)
	durationSeconds := input.DurationSeconds
	if durationSeconds < 0 || durationSeconds > 7*24*60*60 {
		return Status{}, ErrInvalidTrackMetadata
	}
	coverPath := ""
	coverMIME := ""
	sizeBytes := int64(0)
	if status.AlreadyUploaded {
		hash, written, _, err := hashUpload(input.File, io.Discard, s.limitFor(input.IsAdmin))
		if err != nil {
			return Status{}, err
		}
		if hash != sha {
			return Status{}, ErrHashMismatch
		}
		sizeBytes = written
		var existingPath string
		if err := s.db.QueryRowContext(ctx, `select size_bytes, stored_path, metadata_title, duration_seconds, cover_path, cover_mime_type from audio_files where sha256 = ?`, sha).Scan(&sizeBytes, &existingPath, &metadataTitle, &durationSeconds, &coverPath, &coverMIME); err != nil {
			return Status{}, fmt.Errorf("read existing audio size: %w", err)
		}
		path = existingPath
	} else {
		hash, written, detected, err := s.writeFile(sha, media.ext, input.File, s.limitFor(input.IsAdmin))
		if err != nil {
			return Status{}, err
		}
		if hash != sha {
			_ = os.Remove(path)
			return Status{}, ErrHashMismatch
		}
		if !media.matchesDetected(detected) {
			_ = os.Remove(path)
			return Status{}, ErrUnsupportedFile
		}
		sizeBytes = written
		if input.Cover != nil {
			var err error
			coverPath, coverMIME, err = s.writeCover(sha, input.CoverMIMEType, input.Cover)
			if err != nil {
				_ = os.Remove(path)
				return Status{}, err
			}
		}
	}

	trackID, err := randomID()
	if err != nil {
		return Status{}, err
	}
	now := nowText()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Status{}, fmt.Errorf("begin audio store: %w", err)
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(ctx, `insert into audio_files (sha256, ext, size_bytes, mime_type, stored_path, metadata_title, duration_seconds, cover_path, cover_mime_type, uploaded_by_user_id, created_at, missing_at)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, null)
		on conflict(sha256) do update set missing_at = null`, sha, media.ext, sizeBytes, media.mime, path, metadataTitle, durationSeconds, coverPath, coverMIME, userID, now); err != nil {
		return Status{}, fmt.Errorf("upsert audio file: %w", err)
	}
	var nextOrder int
	if err := tx.QueryRowContext(ctx, `select coalesce(max(display_order) + 1, 0) from room_audio_tracks where room_id = ?`, input.RoomID).Scan(&nextOrder); err != nil {
		return Status{}, fmt.Errorf("next audio order: %w", err)
	}
	originalName := strings.TrimSpace(input.OriginalName)
	title := titleFromMetadata(originalName, metadataTitle)
	if _, err := tx.ExecContext(ctx, `insert into room_audio_tracks (id, room_id, sha256, original_name, title, uploader_display_name, display_order, uploaded_by_user_id, created_at, updated_at) values (?, ?, ?, ?, ?, '', ?, ?, ?, ?)`, trackID, input.RoomID, sha, originalName, title, nextOrder, userID, now, now); err != nil {
		return Status{}, fmt.Errorf("insert room audio track: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Status{}, fmt.Errorf("commit audio store: %w", err)
	}
	return Status{ID: trackID, SHA256: sha, OriginalName: originalName, Title: title, MetadataTitle: metadataTitle, MIMEType: media.mime, SizeBytes: sizeBytes, DurationSeconds: durationSeconds, HasCover: coverPath != "", UploadedByUserID: userID, AlreadyUploaded: status.AlreadyUploaded}, nil
}

// CurrentRoomFile returns the requested room audio track.
func (s *Service) CurrentRoomFile(ctx context.Context, roomID string, trackID string) (RoomFile, error) {
	var file RoomFile
	var missingAt sql.NullString
	err := s.db.QueryRowContext(ctx, `select rat.id, rat.sha256, rat.original_name, af.stored_path, af.mime_type, af.missing_at
		from room_audio_tracks rat join audio_files af on af.sha256 = rat.sha256
		where rat.room_id = ? and rat.id = ?`, roomID, trackID).Scan(&file.ID, &file.SHA256, &file.OriginalName, &file.StoredPath, &file.MIMEType, &missingAt)
	if errors.Is(err, sql.ErrNoRows) {
		return RoomFile{}, ErrNoRoomAudio
	}
	if err != nil {
		return RoomFile{}, fmt.Errorf("read room audio file: %w", err)
	}
	if _, err := os.Stat(file.StoredPath); errors.Is(err, os.ErrNotExist) {
		if !missingAt.Valid {
			if _, err := s.db.ExecContext(ctx, `update audio_files set missing_at = ? where sha256 = ?`, nowText(), file.SHA256); err != nil {
				return RoomFile{}, fmt.Errorf("mark audio missing: %w", err)
			}
		}
		return RoomFile{}, ErrMissingFile
	} else if err != nil {
		return RoomFile{}, fmt.Errorf("stat audio file: %w", err)
	}
	return file, nil
}

// CurrentRoomFileByToken returns the requested file when token is valid for the track.
func (s *Service) CurrentRoomFileByToken(ctx context.Context, roomID string, trackID string, token string) (RoomFile, error) {
	tokenHash := hashToken(strings.TrimSpace(token))
	if tokenHash == "" {
		return RoomFile{}, ErrInvalidDownloadToken
	}
	var found string
	err := s.db.QueryRowContext(ctx, `select token_hash from audio_download_tokens where token_hash = ? and room_id = ? and track_id = ?`, tokenHash, roomID, trackID).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return RoomFile{}, ErrInvalidDownloadToken
	}
	if err != nil {
		return RoomFile{}, fmt.Errorf("read audio download token: %w", err)
	}
	return s.CurrentRoomFile(ctx, roomID, trackID)
}

// IssueDownloadToken creates a durable bearer token for one room audio track.
func (s *Service) IssueDownloadToken(ctx context.Context, roomID string, trackID string, createdByUserID string) (string, error) {
	if _, err := s.CurrentRoomFile(ctx, roomID, trackID); err != nil {
		return "", err
	}
	token, err := randomSecret(32)
	if err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `insert into audio_download_tokens (token_hash, room_id, track_id, created_by_user_id, created_at) values (?, ?, ?, ?, ?)`, hashToken(token), roomID, trackID, createdByUserID, nowText()); err != nil {
		return "", fmt.Errorf("insert audio download token: %w", err)
	}
	return token, nil
}

// CoverFile returns cover art for a stored room audio track.
func (s *Service) CoverFile(ctx context.Context, roomID string, trackID string) (RoomFile, error) {
	var file RoomFile
	err := s.db.QueryRowContext(ctx, `select rat.id, rat.sha256, rat.original_name, af.cover_path, af.cover_mime_type
		from room_audio_tracks rat join audio_files af on af.sha256 = rat.sha256
		where rat.room_id = ? and rat.id = ? and af.cover_path <> ''`, roomID, trackID).Scan(&file.ID, &file.SHA256, &file.OriginalName, &file.StoredPath, &file.MIMEType)
	if errors.Is(err, sql.ErrNoRows) {
		return RoomFile{}, ErrNoRoomAudio
	}
	if err != nil {
		return RoomFile{}, fmt.Errorf("read audio cover: %w", err)
	}
	if _, err := os.Stat(file.StoredPath); errors.Is(err, os.ErrNotExist) {
		return RoomFile{}, ErrMissingFile
	} else if err != nil {
		return RoomFile{}, fmt.Errorf("stat audio cover: %w", err)
	}
	return file, nil
}

// UpdateTrackMetadata edits user-visible room audio metadata.
func (s *Service) UpdateTrackMetadata(ctx context.Context, roomID string, trackID string, userID string, isMod bool, title *string, uploaderDisplayName *string) error {
	var uploader string
	err := s.db.QueryRowContext(ctx, `select uploaded_by_user_id from room_audio_tracks where room_id = ? and id = ?`, roomID, trackID).Scan(&uploader)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoRoomAudio
	}
	if err != nil {
		return fmt.Errorf("read audio track metadata: %w", err)
	}
	if !isMod && uploader != userID {
		return ErrNotTrackUploaderOrMod
	}
	if uploaderDisplayName != nil && !isMod {
		return ErrNotTrackUploaderOrMod
	}
	now := nowText()
	if title != nil {
		clean := trimLimit(*title, 200)
		if clean == "" {
			return ErrInvalidTrackMetadata
		}
		if _, err := s.db.ExecContext(ctx, `update room_audio_tracks set title = ?, updated_at = ? where room_id = ? and id = ?`, clean, now, roomID, trackID); err != nil {
			return fmt.Errorf("update audio title: %w", err)
		}
	}
	if uploaderDisplayName != nil {
		clean := trimLimit(*uploaderDisplayName, 80)
		if _, err := s.db.ExecContext(ctx, `update room_audio_tracks set uploader_display_name = ?, updated_at = ? where room_id = ? and id = ?`, clean, now, roomID, trackID); err != nil {
			return fmt.Errorf("update audio uploader display: %w", err)
		}
	}
	return nil
}

// ListTracks returns the room audio playlist in display order.
func (s *Service) ListTracks(ctx context.Context, roomID string) ([]Track, error) {
	rows, err := s.db.QueryContext(ctx, `select rat.id, rat.sha256, rat.original_name, rat.title, af.metadata_title, af.mime_type, af.size_bytes, af.duration_seconds, af.cover_path, rat.uploaded_by_user_id, u.display_name, rat.uploader_display_name, rat.display_order, af.stored_path, af.missing_at
		from room_audio_tracks rat join audio_files af on af.sha256 = rat.sha256
		left join users u on u.id = rat.uploaded_by_user_id
		where rat.room_id = ?
		order by rat.display_order asc, rat.created_at asc`, roomID)
	if err != nil {
		return nil, fmt.Errorf("list audio tracks: %w", err)
	}
	defer rows.Close()
	tracks := []Track{}
	for rows.Next() {
		var track Track
		var storedPath string
		var coverPath string
		var missingAt sql.NullString
		if err := rows.Scan(&track.ID, &track.SHA256, &track.OriginalName, &track.Title, &track.MetadataTitle, &track.MIMEType, &track.SizeBytes, &track.DurationSeconds, &coverPath, &track.UploadedByUserID, &track.UploadedByName, &track.UploaderDisplayName, &track.DisplayOrder, &storedPath, &missingAt); err != nil {
			return nil, fmt.Errorf("scan audio track: %w", err)
		}
		track.HasCover = coverPath != ""
		if _, err := os.Stat(storedPath); errors.Is(err, os.ErrNotExist) {
			track.Missing = true
			if !missingAt.Valid {
				if _, err := s.db.ExecContext(ctx, `update audio_files set missing_at = ? where sha256 = ?`, nowText(), track.SHA256); err != nil {
					return nil, fmt.Errorf("mark listed audio missing: %w", err)
				}
			}
		} else if err != nil {
			return nil, fmt.Errorf("stat listed audio: %w", err)
		}
		tracks = append(tracks, track)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audio tracks: %w", err)
	}
	return tracks, nil
}

// Reorder applies a moderator-provided playlist order.
func (s *Service) Reorder(ctx context.Context, roomID string, orderedTrackIDs []string) error {
	tracks, err := s.ListTracks(ctx, roomID)
	if err != nil {
		return err
	}
	if len(orderedTrackIDs) != len(tracks) {
		return fmt.Errorf("audio reorder must include every track")
	}
	known := map[string]bool{}
	for _, track := range tracks {
		known[track.ID] = true
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin audio reorder: %w", err)
	}
	defer rollback(tx)
	now := nowText()
	for index, id := range orderedTrackIDs {
		if !known[id] {
			return fmt.Errorf("unknown audio track in reorder")
		}
		delete(known, id)
		if _, err := tx.ExecContext(ctx, `update room_audio_tracks set display_order = ?, updated_at = ? where room_id = ? and id = ?`, index, now, roomID, id); err != nil {
			return fmt.Errorf("update audio order: %w", err)
		}
	}
	if len(known) != 0 {
		return fmt.Errorf("audio reorder omitted tracks")
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit audio reorder: %w", err)
	}
	return nil
}

// RemoveTrack removes a playlist item. Mods may remove any track; uploaders may remove their own.
func (s *Service) RemoveTrack(ctx context.Context, roomID string, trackID string, userID string, isMod bool) error {
	var uploader string
	err := s.db.QueryRowContext(ctx, `select uploaded_by_user_id from room_audio_tracks where room_id = ? and id = ?`, roomID, trackID).Scan(&uploader)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoRoomAudio
	}
	if err != nil {
		return fmt.Errorf("read audio uploader: %w", err)
	}
	if !isMod && uploader != userID {
		return ErrNotTrackUploaderOrMod
	}
	if _, err := s.db.ExecContext(ctx, `delete from audio_download_tokens where room_id = ? and track_id = ?`, roomID, trackID); err != nil {
		return fmt.Errorf("delete audio download tokens: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `delete from room_audio_tracks where room_id = ? and id = ?`, roomID, trackID); err != nil {
		return fmt.Errorf("delete audio track: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `update rooms set audio_current_track_id = null, audio_state = 'paused', audio_position_seconds = 0, audio_started_at = null where id = ? and audio_current_track_id = ?`, roomID, trackID); err != nil {
		return fmt.Errorf("clear current audio track: %w", err)
	}
	return nil
}

// Cleanup deletes audio tracks for rooms older than gcAfter and removes orphaned files.
func (s *Service) Cleanup(ctx context.Context, at time.Time, gcAfter time.Duration) error {
	cutoff := at.UTC().Add(-gcAfter).Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx, `delete from audio_download_tokens where room_id in (select id from rooms where created_at <= ?)`, cutoff); err != nil {
		return fmt.Errorf("delete expired audio download tokens: %w", err)
	}
	if _, err := s.db.ExecContext(ctx, `delete from room_audio_tracks where room_id in (select id from rooms where created_at <= ?)`, cutoff); err != nil {
		return fmt.Errorf("delete expired room audio tracks: %w", err)
	}
	rows, err := s.db.QueryContext(ctx, `select af.sha256, af.stored_path from audio_files af where not exists (select 1 from room_audio_tracks rat where rat.sha256 = af.sha256)`)
	if err != nil {
		return fmt.Errorf("list orphan audio files: %w", err)
	}
	defer rows.Close()
	type candidate struct {
		sha  string
		path string
	}
	var candidates []candidate
	for rows.Next() {
		var item candidate
		if err := rows.Scan(&item.sha, &item.path); err != nil {
			return fmt.Errorf("scan orphan audio file: %w", err)
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate orphan audio files: %w", err)
	}
	for _, item := range candidates {
		if err := os.Remove(item.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete audio file: %w", err)
		}
		coverPath := ""
		if err := s.db.QueryRowContext(ctx, `select cover_path from audio_files where sha256 = ?`, item.sha).Scan(&coverPath); err == nil && coverPath != "" {
			if err := os.Remove(coverPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("delete audio cover: %w", err)
			}
		}
		if _, err := s.db.ExecContext(ctx, `delete from audio_files where sha256 = ?`, item.sha); err != nil {
			return fmt.Errorf("delete audio file row: %w", err)
		}
	}
	return nil
}

func (s *Service) status(ctx context.Context, sha string) (Status, error) {
	status := Status{SHA256: sha}
	var storedPath string
	var missingAt sql.NullString
	err := s.db.QueryRowContext(ctx, `select size_bytes, mime_type, stored_path, uploaded_by_user_id, missing_at from audio_files where sha256 = ?`, sha).Scan(&status.SizeBytes, &status.MIMEType, &storedPath, &status.UploadedByUserID, &missingAt)
	if errors.Is(err, sql.ErrNoRows) {
		return status, nil
	}
	if err != nil {
		return Status{}, fmt.Errorf("read audio status: %w", err)
	}
	status.AlreadyUploaded = true
	if _, err := os.Stat(storedPath); errors.Is(err, os.ErrNotExist) {
		status.Missing = true
		if !missingAt.Valid {
			if _, err := s.db.ExecContext(ctx, `update audio_files set missing_at = ? where sha256 = ?`, nowText(), sha); err != nil {
				return Status{}, fmt.Errorf("mark audio missing: %w", err)
			}
		}
		return status, nil
	} else if err != nil {
		return Status{}, fmt.Errorf("stat audio: %w", err)
	}
	return status, nil
}

func (s *Service) limitFor(isAdmin bool) int64 {
	if isAdmin {
		return 1 << 62
	}
	return s.maxBytes
}

func (s *Service) writeFile(sha string, ext string, reader io.Reader, maxBytes int64) (string, int64, string, error) {
	path := s.pathFor(sha, ext)
	tmp, err := os.CreateTemp(s.audioDir, sha+".*.tmp")
	if err != nil {
		return "", 0, "", fmt.Errorf("create temp audio: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	hash, written, detected, err := hashUpload(reader, tmp, maxBytes)
	if err != nil {
		return "", 0, "", err
	}
	freeBytes, err := s.freeSpace(s.audioDir)
	if err != nil {
		return "", 0, "", err
	}
	if freeBytes-written < s.minFreeBytes {
		return "", 0, "", ErrInsufficientFreeSpace
	}
	if err := tmp.Close(); err != nil {
		return "", 0, "", fmt.Errorf("close temp audio: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", 0, "", fmt.Errorf("store audio: %w", err)
	}
	return hash, written, detected, nil
}

func (s *Service) writeCover(sha string, mimeType string, reader io.Reader) (string, string, error) {
	mime := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	ext := "bin"
	switch mime {
	case "image/jpeg", "image/jpg":
		mime = "image/jpeg"
		ext = "jpg"
	case "image/png":
		ext = "png"
	case "image/webp":
		ext = "webp"
	default:
		return "", "", ErrInvalidTrackMetadata
	}
	path := filepath.Join(s.audioDir, sha+".cover."+ext)
	tmp, err := os.CreateTemp(s.audioDir, sha+".cover.*.tmp")
	if err != nil {
		return "", "", fmt.Errorf("create temp audio cover: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	limited := &limitReader{reader: reader, remaining: 2<<20 + 1}
	written, err := io.Copy(tmp, limited)
	if err != nil {
		return "", "", fmt.Errorf("read audio cover: %w", err)
	}
	if written > 2<<20 {
		return "", "", ErrInvalidTrackMetadata
	}
	if err := tmp.Close(); err != nil {
		return "", "", fmt.Errorf("close temp audio cover: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", "", fmt.Errorf("store audio cover: %w", err)
	}
	return path, mime, nil
}

func hashUpload(reader io.Reader, writer io.Writer, maxBytes int64) (string, int64, string, error) {
	hasher := sha256.New()
	detector := &contentDetector{}
	limited := &limitReader{reader: reader, remaining: maxBytes + 1}
	written, err := io.Copy(io.MultiWriter(writer, hasher, detector), limited)
	if err != nil {
		return "", 0, "", fmt.Errorf("read audio upload: %w", err)
	}
	if written > maxBytes {
		return "", 0, "", ErrTooLarge
	}
	return hex.EncodeToString(hasher.Sum(nil)), written, detector.MIME(), nil
}

type limitReader struct {
	reader    io.Reader
	remaining int64
}

func (r *limitReader) Read(p []byte) (int, error) {
	if r.remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > r.remaining {
		p = p[:r.remaining]
	}
	n, err := r.reader.Read(p)
	r.remaining -= int64(n)
	return n, err
}

func mediaFor(originalName string, mimeType string) (mediaType, error) {
	mime := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	ext := strings.TrimPrefix(filepath.Ext(strings.ToLower(strings.TrimSpace(originalName))), ".")
	if ext == "" {
		ext = "audio"
	}
	switch {
	case mime == "" || mime == octetMIME:
		fallback, ok := audioMIMEForExtension(ext)
		if !ok {
			return mediaType{}, ErrUnsupportedFile
		}
		mime = fallback
	case mime == "application/ogg" && audioExtension(ext):
		mime = "audio/ogg"
	case strings.HasPrefix(mime, "audio/"):
		mime = normalizeAudioMIME(ext, mime)
	case (mime == "video/mp4" || mime == "application/mp4") && mp4AudioExtension(ext):
		mime = "audio/mp4"
	default:
		return mediaType{}, ErrUnsupportedFile
	}
	return mediaType{ext: ext, mime: mime}, nil
}

type mediaType struct {
	ext  string
	mime string
}

func (m mediaType) matchesDetected(detected string) bool {
	if detected == "" || detected == octetMIME {
		return true
	}
	if detected == "video/mp4" && mp4AudioExtension(m.ext) {
		return true
	}
	return strings.HasPrefix(detected, "audio/") || detected == "application/ogg"
}

func normalizeAudioMIME(ext string, mime string) string {
	if (mime == "audio/m4a" || mime == "audio/x-m4a" || mime == "audio/aac") && mp4AudioExtension(ext) {
		return "audio/mp4"
	}
	return mime
}

func audioMIMEForExtension(ext string) (string, bool) {
	switch ext {
	case "mp3":
		return "audio/mpeg", true
	case "m4a", "m4b":
		return "audio/mp4", true
	case "aac":
		return "audio/aac", true
	case "ogg", "oga":
		return "audio/ogg", true
	case "opus":
		return "audio/opus", true
	case "wav":
		return "audio/wav", true
	case "flac":
		return "audio/flac", true
	case "webm", "weba":
		return "audio/webm", true
	default:
		return "", false
	}
}

func audioExtension(ext string) bool {
	_, ok := audioMIMEForExtension(ext)
	return ok
}

func mp4AudioExtension(ext string) bool {
	return ext == "m4a" || ext == "m4b"
}

type contentDetector struct {
	prefix []byte
}

func (d *contentDetector) Write(p []byte) (int, error) {
	remaining := detectPrefix - len(d.prefix)
	if remaining > 0 {
		if len(p) < remaining {
			remaining = len(p)
		}
		d.prefix = append(d.prefix, p[:remaining]...)
	}
	return len(p), nil
}

func (d *contentDetector) MIME() string {
	if len(d.prefix) == 0 {
		return ""
	}
	return http.DetectContentType(d.prefix)
}

func (s *Service) pathFor(sha string, ext string) string {
	return filepath.Join(s.audioDir, sha+"."+ext)
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func titleFromMetadata(originalName string, metadataTitle string) string {
	if clean := trimLimit(metadataTitle, 200); clean != "" {
		return clean
	}
	name := strings.TrimSpace(originalName)
	ext := filepath.Ext(name)
	if ext != "" {
		name = strings.TrimSuffix(name, ext)
	}
	if clean := trimLimit(name, 200); clean != "" {
		return clean
	}
	return "Untitled audio"
}

func trimLimit(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) > maxRunes {
		value = string(runes[:maxRunes])
	}
	return strings.TrimSpace(value)
}

func hashToken(rawToken string) string {
	if rawToken == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(sum[:])
}

func randomID() (string, error) {
	return randomSecret(12)
}

func randomSecret(byteCount int) (string, error) {
	bytes := make([]byte, 12)
	if byteCount > 0 {
		bytes = make([]byte, byteCount)
	}
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("random id: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func filesystemFreeSpace(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("stat filesystem: %w", err)
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

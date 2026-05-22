// Package slides manages local PDF storage and per-room slide references.
package slides

import (
	"context"
	"crypto/sha256"
	"database/sql"
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
	pdfExt       = "pdf"
	octetMIME    = "application/octet-stream"
	pdfMIME      = "application/pdf"
	pngMIME      = "image/png"
	jpegMIME     = "image/jpeg"
	webpMIME     = "image/webp"
	gifMIME      = "image/gif"
	detectPrefix = 512
)

// Service stores slide PDFs under a configured local directory.
type Service struct {
	db           *store.DB
	slideDir     string
	maxBytes     int64
	minFreeBytes int64
	freeSpace    func(string) (int64, error)
}

// StoreInput is the upload or attachment payload.
type StoreInput struct {
	RoomID       string
	SHA256       string
	OriginalName string
	ExpiresAt    time.Time
	MIMEType     string
	File         io.Reader
}

// Status describes a stored slide hash.
type Status struct {
	Exists          bool   `json:"exists"`
	SHA256          string `json:"sha256"`
	AlreadyUploaded bool   `json:"alreadyUploaded"`
	Missing         bool   `json:"missing"`
	RoomID          string `json:"-"`
}

// RoomFile describes the current PDF file attached to a room.
type RoomFile struct {
	SHA256       string
	OriginalName string
	StoredPath   string
	MIMEType     string
}

var (
	ErrUnsupportedFile       = errors.New("only PDF, PNG, JPEG, WebP, and GIF slide files are supported")
	ErrHashMismatch          = errors.New("slide hash does not match uploaded file")
	ErrInvalidHash           = errors.New("slide hash must be a SHA-256 hex digest")
	ErrMissingFile           = errors.New("slide file was deleted manually")
	ErrFileRequired          = errors.New("slide file is required")
	ErrInvalidExpiry         = errors.New("slide expiration must be in the future")
	ErrTooLarge              = errors.New("slide file exceeds size limit")
	ErrNoRoomSlide           = errors.New("room has no slide file")
	ErrInsufficientFreeSpace = errors.New("upload would leave too little free disk space")
)

// NewService creates the slide directory and returns a service.
func NewService(db *store.DB, slideDir string, maxBytes int64) (*Service, error) {
	return NewServiceWithMinFree(db, slideDir, maxBytes, 0)
}

// NewServiceWithMinFree creates the slide directory and enforces a post-upload free-space floor.
func NewServiceWithMinFree(db *store.DB, slideDir string, maxBytes int64, minFreeBytes int64) (*Service, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("slide max bytes must be positive")
	}
	if minFreeBytes < 0 {
		return nil, fmt.Errorf("minimum free space must not be negative")
	}
	if err := os.MkdirAll(slideDir, 0o700); err != nil {
		return nil, fmt.Errorf("create slide dir: %w", err)
	}
	return &Service{db: db, slideDir: slideDir, maxBytes: maxBytes, minFreeBytes: minFreeBytes, freeSpace: filesystemFreeSpace}, nil
}

// MaxBytes returns the configured upload size limit.
func (s *Service) MaxBytes() int64 {
	return s.maxBytes
}

// Status reports whether a slide hash is known, present, or manually missing.
func (s *Service) Status(ctx context.Context, sha string) (Status, error) {
	if !validSHA256(sha) {
		return Status{}, ErrInvalidHash
	}
	status := Status{SHA256: sha}
	var storedPath string
	var missingAt sql.NullString
	err := s.db.QueryRowContext(ctx, `select stored_path, missing_at from slide_files where sha256 = ?`, sha).Scan(&storedPath, &missingAt)
	if errors.Is(err, sql.ErrNoRows) {
		return status, nil
	}
	if err != nil {
		return Status{}, fmt.Errorf("read slide status: %w", err)
	}
	status.Exists = true
	status.AlreadyUploaded = true
	if _, err := os.Stat(storedPath); errors.Is(err, os.ErrNotExist) {
		status.Missing = true
		if !missingAt.Valid {
			if _, err := s.db.ExecContext(ctx, `update slide_files set missing_at = ? where sha256 = ?`, nowText(), sha); err != nil {
				return Status{}, fmt.Errorf("mark slide missing: %w", err)
			}
		}
		return status, nil
	} else if err != nil {
		return Status{}, fmt.Errorf("stat slide: %w", err)
	}
	if missingAt.Valid {
		if _, err := s.db.ExecContext(ctx, `update slide_files set missing_at = null where sha256 = ?`, sha); err != nil {
			return Status{}, fmt.Errorf("clear missing marker: %w", err)
		}
	}
	return status, nil
}

// CurrentRoomFile returns the current room PDF unless it is absent or manually missing.
func (s *Service) CurrentRoomFile(ctx context.Context, roomID string) (RoomFile, error) {
	var file RoomFile
	var missingAt sql.NullString
	err := s.db.QueryRowContext(
		ctx,
		`select rs.sha256, rs.original_name, sf.stored_path, sf.mime_type, sf.missing_at
		 from room_slides rs
		 join slide_files sf on sf.sha256 = rs.sha256
		 where rs.room_id = ?`,
		roomID,
	).Scan(&file.SHA256, &file.OriginalName, &file.StoredPath, &file.MIMEType, &missingAt)
	if errors.Is(err, sql.ErrNoRows) {
		return RoomFile{}, ErrNoRoomSlide
	}
	if err != nil {
		return RoomFile{}, fmt.Errorf("read room slide file: %w", err)
	}
	if _, err := os.Stat(file.StoredPath); errors.Is(err, os.ErrNotExist) {
		if !missingAt.Valid {
			if _, err := s.db.ExecContext(ctx, `update slide_files set missing_at = ? where sha256 = ?`, nowText(), file.SHA256); err != nil {
				return RoomFile{}, fmt.Errorf("mark room slide missing: %w", err)
			}
		}
		return RoomFile{}, ErrMissingFile
	} else if err != nil {
		return RoomFile{}, fmt.Errorf("stat room slide file: %w", err)
	}
	return file, nil
}

// Store validates and stores a PDF, then upserts the room slide reference.
func (s *Service) Store(ctx context.Context, userID string, input StoreInput) (Status, error) {
	sha := strings.ToLower(strings.TrimSpace(input.SHA256))
	if !validSHA256(sha) {
		return Status{}, ErrInvalidHash
	}
	media, err := mediaFor(input.OriginalName, input.MIMEType)
	if err != nil {
		return Status{}, ErrUnsupportedFile
	}
	if !input.ExpiresAt.After(time.Now().UTC()) {
		return Status{}, ErrInvalidExpiry
	}

	status, err := s.Status(ctx, sha)
	if err != nil {
		return Status{}, err
	}
	if status.Missing {
		return Status{}, ErrMissingFile
	}
	path := s.pathFor(sha, media.ext)
	sizeBytes := int64(0)
	if input.File == nil {
		if !status.AlreadyUploaded {
			return Status{}, ErrFileRequired
		}
		var existingSize int64
		if err := s.db.QueryRowContext(ctx, `select size_bytes, stored_path from slide_files where sha256 = ?`, sha).Scan(&existingSize, &path); err != nil {
			return Status{}, fmt.Errorf("read existing slide size: %w", err)
		}
		sizeBytes = existingSize
	} else if status.AlreadyUploaded {
		hash, written, detected, err := hashUpload(input.File, io.Discard, s.maxBytes)
		if err != nil {
			return Status{}, err
		}
		if hash != sha {
			return Status{}, ErrHashMismatch
		}
		if !media.matchesDetected(detected) {
			return Status{}, ErrUnsupportedFile
		}
		sizeBytes = written
	} else {
		hash, written, detected, err := s.writeFile(sha, media.ext, input.File)
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
	}

	now := nowText()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Status{}, fmt.Errorf("begin slide store: %w", err)
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(
		ctx,
		`insert into slide_files (sha256, ext, size_bytes, mime_type, stored_path, uploaded_by_user_id, created_at, missing_at)
		 values (?, ?, ?, ?, ?, ?, ?, null)
		 on conflict(sha256) do update set missing_at = null`,
		sha,
		media.ext,
		sizeBytes,
		media.mime,
		path,
		userID,
		now,
	); err != nil {
		return Status{}, fmt.Errorf("upsert slide file: %w", err)
	}
	if _, err := tx.ExecContext(
		ctx,
		`insert into room_slides (room_id, sha256, original_name, expires_at, uploaded_by_user_id, created_at, updated_at)
		 values (?, ?, ?, ?, ?, ?, ?)
		 on conflict(room_id) do update set sha256 = excluded.sha256, original_name = excluded.original_name, expires_at = excluded.expires_at, uploaded_by_user_id = excluded.uploaded_by_user_id, updated_at = excluded.updated_at`,
		input.RoomID,
		sha,
		strings.TrimSpace(input.OriginalName),
		input.ExpiresAt.UTC().Format(time.RFC3339Nano),
		userID,
		now,
		now,
	); err != nil {
		return Status{}, fmt.Errorf("upsert room slide: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Status{}, fmt.Errorf("commit slide store: %w", err)
	}
	return Status{Exists: true, SHA256: sha, AlreadyUploaded: status.AlreadyUploaded, Missing: false, RoomID: input.RoomID}, nil
}

// UpdateRoomExpiration changes the current room slide expiration.
func (s *Service) UpdateRoomExpiration(ctx context.Context, roomID string, expiresAt time.Time) error {
	if !expiresAt.After(time.Now().UTC()) {
		return ErrInvalidExpiry
	}
	result, err := s.db.ExecContext(ctx, `update room_slides set expires_at = ?, updated_at = ? where room_id = ?`, expiresAt.UTC().Format(time.RFC3339Nano), nowText(), roomID)
	if err != nil {
		return fmt.Errorf("update slide expiration: %w", err)
	}
	if changed, err := result.RowsAffected(); err == nil && changed == 0 {
		return ErrNoRoomSlide
	}
	return nil
}

// RemoveRoomSlide removes the room slide reference without deleting the physical file.
func (s *Service) RemoveRoomSlide(ctx context.Context, roomID string) error {
	result, err := s.db.ExecContext(ctx, `delete from room_slides where room_id = ?`, roomID)
	if err != nil {
		return fmt.Errorf("remove room slide: %w", err)
	}
	if changed, err := result.RowsAffected(); err == nil && changed == 0 {
		return ErrNoRoomSlide
	}
	return nil
}

// Cleanup deletes expired room references and files that no unexpired references need.
func (s *Service) Cleanup(ctx context.Context, at time.Time) error {
	now := at.UTC().Format(time.RFC3339Nano)
	if _, err := s.db.ExecContext(ctx, `delete from room_slides where expires_at <= ?`, now); err != nil {
		return fmt.Errorf("delete expired slide references: %w", err)
	}
	rows, err := s.db.QueryContext(
		ctx,
		`select sf.sha256, sf.stored_path
		 from slide_files sf
		 where not exists (select 1 from room_slides rs where rs.sha256 = sf.sha256 and rs.expires_at > ?)`,
		now,
	)
	if err != nil {
		return fmt.Errorf("list orphan slide files: %w", err)
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
			return fmt.Errorf("scan orphan slide file: %w", err)
		}
		candidates = append(candidates, item)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate orphan slide files: %w", err)
	}
	for _, item := range candidates {
		if err := os.Remove(item.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete slide file: %w", err)
		}
		if _, err := s.db.ExecContext(ctx, `delete from slide_files where sha256 = ?`, item.sha); err != nil {
			return fmt.Errorf("delete slide file row: %w", err)
		}
	}
	return nil
}

func (s *Service) writeFile(sha string, ext string, reader io.Reader) (string, int64, string, error) {
	path := s.pathFor(sha, ext)
	tmp, err := os.CreateTemp(s.slideDir, sha+".*.tmp")
	if err != nil {
		return "", 0, "", fmt.Errorf("create temp slide: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	hash, written, detected, err := hashUpload(reader, tmp, s.maxBytes)
	if err != nil {
		return "", 0, "", err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, "", fmt.Errorf("close temp slide: %w", err)
	}
	freeBytes, err := s.freeSpace(s.slideDir)
	if err != nil {
		return "", 0, "", err
	}
	if freeBytes-written < s.minFreeBytes {
		return "", 0, "", ErrInsufficientFreeSpace
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", 0, "", fmt.Errorf("store slide: %w", err)
	}
	return hash, written, detected, nil
}

func hashUpload(reader io.Reader, writer io.Writer, maxBytes int64) (string, int64, string, error) {
	hasher := sha256.New()
	detector := &contentDetector{}
	limited := &limitReader{reader: reader, remaining: maxBytes + 1}
	written, err := io.Copy(io.MultiWriter(writer, hasher, detector), limited)
	if err != nil {
		return "", 0, "", fmt.Errorf("read slide upload: %w", err)
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

func (s *Service) pathFor(sha string, ext string) string {
	return filepath.Join(s.slideDir, sha+"."+ext)
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

type mediaType struct {
	ext  string
	mime string
}

func mediaFor(originalName string, mimeType string) (mediaType, error) {
	ext := strings.TrimPrefix(filepath.Ext(strings.ToLower(strings.TrimSpace(originalName))), ".")
	byExt := map[string]string{
		pdfExt: "application/pdf",
		"png":  pngMIME,
		"jpg":  jpegMIME,
		"jpeg": jpegMIME,
		"webp": webpMIME,
		"gif":  gifMIME,
	}
	expected, ok := byExt[ext]
	if !ok {
		return mediaType{}, ErrUnsupportedFile
	}
	mime := strings.ToLower(strings.TrimSpace(strings.Split(mimeType, ";")[0]))
	if mime == "" || mime == octetMIME {
		mime = expected
	}
	if mime != expected {
		return mediaType{}, ErrUnsupportedFile
	}
	return mediaType{ext: ext, mime: expected}, nil
}

func (m mediaType) matchesDetected(detected string) bool {
	if detected == "" || detected == octetMIME {
		return false
	}
	if m.mime == jpegMIME {
		return detected == jpegMIME
	}
	return detected == m.mime
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

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func filesystemFreeSpace(path string) (int64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("stat filesystem: %w", err)
	}
	return int64(stat.Bavail) * int64(stat.Bsize), nil
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

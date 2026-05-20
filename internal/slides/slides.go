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
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/NightMachinery/SlideTalk/internal/store"
)

const pdfExt = ".pdf"

// Service stores slide PDFs under a configured local directory.
type Service struct {
	db       *store.DB
	slideDir string
	maxBytes int64
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
}

// RoomFile describes the current PDF file attached to a room.
type RoomFile struct {
	SHA256       string
	OriginalName string
	StoredPath   string
}

var (
	ErrUnsupportedFile = errors.New("only PDF slide files are supported")
	ErrHashMismatch    = errors.New("slide hash does not match uploaded file")
	ErrInvalidHash     = errors.New("slide hash must be a SHA-256 hex digest")
	ErrMissingFile     = errors.New("slide file was deleted manually")
	ErrFileRequired    = errors.New("slide file is required")
	ErrInvalidExpiry   = errors.New("slide expiration must be in the future")
	ErrTooLarge        = errors.New("slide file exceeds size limit")
	ErrNoRoomSlide     = errors.New("room has no slide file")
)

// NewService creates the slide directory and returns a service.
func NewService(db *store.DB, slideDir string, maxBytes int64) (*Service, error) {
	if maxBytes <= 0 {
		return nil, fmt.Errorf("slide max bytes must be positive")
	}
	if err := os.MkdirAll(slideDir, 0o700); err != nil {
		return nil, fmt.Errorf("create slide dir: %w", err)
	}
	return &Service{db: db, slideDir: slideDir, maxBytes: maxBytes}, nil
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
		`select rs.sha256, rs.original_name, sf.stored_path, sf.missing_at
		 from room_slides rs
		 join slide_files sf on sf.sha256 = rs.sha256
		 where rs.room_id = ?`,
		roomID,
	).Scan(&file.SHA256, &file.OriginalName, &file.StoredPath, &missingAt)
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
	if filepath.Ext(strings.ToLower(input.OriginalName)) != pdfExt {
		return Status{}, ErrUnsupportedFile
	}
	if input.MIMEType != "" && input.MIMEType != "application/pdf" && input.MIMEType != "application/octet-stream" {
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
	path := s.pathFor(sha)
	sizeBytes := int64(0)
	if input.File == nil {
		if !status.AlreadyUploaded {
			return Status{}, ErrFileRequired
		}
		var existingSize int64
		if err := s.db.QueryRowContext(ctx, `select size_bytes from slide_files where sha256 = ?`, sha).Scan(&existingSize); err != nil {
			return Status{}, fmt.Errorf("read existing slide size: %w", err)
		}
		sizeBytes = existingSize
	} else if status.AlreadyUploaded {
		hash, written, err := hashUpload(input.File, io.Discard, s.maxBytes)
		if err != nil {
			return Status{}, err
		}
		if hash != sha {
			return Status{}, ErrHashMismatch
		}
		sizeBytes = written
	} else {
		hash, written, err := s.writeFile(sha, input.File)
		if err != nil {
			return Status{}, err
		}
		if hash != sha {
			_ = os.Remove(path)
			return Status{}, ErrHashMismatch
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
		"pdf",
		sizeBytes,
		mimeFor(input.MIMEType),
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
	return Status{Exists: true, SHA256: sha, AlreadyUploaded: status.AlreadyUploaded, Missing: false}, nil
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

func (s *Service) writeFile(sha string, reader io.Reader) (string, int64, error) {
	path := s.pathFor(sha)
	tmp, err := os.CreateTemp(s.slideDir, sha+".*.tmp")
	if err != nil {
		return "", 0, fmt.Errorf("create temp slide: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
	}()
	hash, written, err := hashUpload(reader, tmp, s.maxBytes)
	if err != nil {
		return "", 0, err
	}
	if err := tmp.Close(); err != nil {
		return "", 0, fmt.Errorf("close temp slide: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", 0, fmt.Errorf("store slide: %w", err)
	}
	return hash, written, nil
}

func hashUpload(reader io.Reader, writer io.Writer, maxBytes int64) (string, int64, error) {
	hasher := sha256.New()
	limited := &limitReader{reader: reader, remaining: maxBytes + 1}
	written, err := io.Copy(io.MultiWriter(writer, hasher), limited)
	if err != nil {
		return "", 0, fmt.Errorf("read slide upload: %w", err)
	}
	if written > maxBytes {
		return "", 0, ErrTooLarge
	}
	return hex.EncodeToString(hasher.Sum(nil)), written, nil
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

func (s *Service) pathFor(sha string) string {
	return filepath.Join(s.slideDir, sha+pdfExt)
}

func validSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func mimeFor(value string) string {
	if value == "" {
		return "application/pdf"
	}
	return value
}

func nowText() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

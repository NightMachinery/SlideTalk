package main

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/NightMachinery/SlideTalk/internal/audio"
	"github.com/NightMachinery/SlideTalk/internal/auth"
	"github.com/NightMachinery/SlideTalk/internal/config"
	"github.com/NightMachinery/SlideTalk/internal/httpserver"
	"github.com/NightMachinery/SlideTalk/internal/realtime"
	"github.com/NightMachinery/SlideTalk/internal/rooms"
	"github.com/NightMachinery/SlideTalk/internal/slides"
	"github.com/NightMachinery/SlideTalk/internal/store"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "ls":
			return runListRooms(cfg, os.Args[2:])
		case "rm":
			return runRemoveRoomFiles(cfg, os.Args[2:])
		}
	}

	if err := os.MkdirAll(cfg.DataDir, 0o700); err != nil {
		return err
	}
	db, err := store.Open(cfg.DataDir)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := store.Migrate(context.Background(), db.DB); err != nil {
		return err
	}
	authService := auth.NewService(db, cfg.DataDir)
	if err := authService.EnsureAdminToken(context.Background()); err != nil {
		return err
	}
	roomService := rooms.NewServiceWithRetention(db, cfg.RoomGCAfter)
	slideService, err := slides.NewServiceWithMinFree(db, filepath.Join(cfg.DataDir, "slides"), cfg.SlideMaxBytes, cfg.MinFreeSpaceBytes)
	if err != nil {
		return err
	}
	audioService, err := audio.NewService(db, filepath.Join(cfg.DataDir, "audio"), cfg.AudioMaxBytes, cfg.MinFreeSpaceBytes)
	if err != nil {
		return err
	}
	if err := slideService.Cleanup(context.Background(), time.Now().UTC()); err != nil {
		return err
	}
	if err := audioService.Cleanup(context.Background(), time.Now().UTC(), cfg.AudioFilesGCAfter); err != nil {
		return err
	}
	cleanupCtx, stopCleanup := context.WithCancel(context.Background())
	defer stopCleanup()
	go runSlideCleanup(cleanupCtx, slideService)
	go runAudioCleanup(cleanupCtx, audioService, cfg.AudioFilesGCAfter)

	server := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpserver.New(httpserver.ServerOptions{StaticDir: filepath.Join("web", "dist"), AuthService: authService, RoomService: roomService, Hub: realtime.NewHub(db, authService, roomService), SlideService: slideService, AudioService: audioService, AudioDriftThresholdSeconds: int(cfg.AudioDriftThreshold / time.Second)}),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("slidetalk listening on http://%s", cfg.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	signalCh := make(chan os.Signal, 1)
	signal.Notify(signalCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(signalCh)

	select {
	case err := <-errCh:
		return err
	case <-signalCh:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return server.Shutdown(ctx)
	}
}

type cliRoom struct {
	ID          string
	Title       string
	CreatorName string
	CreatedAt   string
	ExpiresAt   sql.NullString
	HasPassword bool
	OnlineCount int
	SizeBytes   int64
}

func runListRooms(cfg config.Config, args []string) error {
	flags := flag.NewFlagSet("ls", flag.ContinueOnError)
	sortBy := flags.String("sort", "created-date", "size, created-date, creator-name, or online-count")
	if err := flags.Parse(args); err != nil {
		return err
	}
	db, err := openCLIStore(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	rooms, err := listCLIRooms(context.Background(), db.DB)
	if err != nil {
		return err
	}
	switch *sortBy {
	case "size":
		sort.SliceStable(rooms, func(i, j int) bool { return rooms[i].SizeBytes > rooms[j].SizeBytes })
	case "created-date":
		sort.SliceStable(rooms, func(i, j int) bool { return rooms[i].CreatedAt > rooms[j].CreatedAt })
	case "creator-name":
		sort.SliceStable(rooms, func(i, j int) bool {
			return strings.ToLower(rooms[i].CreatorName) < strings.ToLower(rooms[j].CreatorName)
		})
	case "online-count":
		sort.SliceStable(rooms, func(i, j int) bool { return rooms[i].OnlineCount > rooms[j].OnlineCount })
	default:
		return fmt.Errorf("unknown sort %q", *sortBy)
	}
	publicURL := cliPublicURL(cfg)
	fmt.Printf("%-18s %-24s %-18s %-11s %-20s %-10s %s\n", "ROOM", "TITLE", "CREATOR", "ONLINE", "EXPIRES", "SIZE", "LINK")
	for _, room := range rooms {
		expires := "never"
		if room.ExpiresAt.Valid {
			expires = room.ExpiresAt.String
		}
		password := "open"
		if room.HasPassword {
			password = "protected"
		}
		link := room.ID
		if publicURL != "" {
			link = strings.TrimRight(publicURL, "/") + "/?room=" + room.ID
		}
		fmt.Printf("%-18s %-24s %-18s %-11d %-20s %-10s %s (%s)\n", room.ID, truncate(room.Title, 24), truncate(room.CreatorName, 18), room.OnlineCount, truncate(expires, 20), formatBytes(room.SizeBytes), link, password)
	}
	return nil
}

func listCLIRooms(ctx context.Context, db *sql.DB) ([]cliRoom, error) {
	rows, err := db.QueryContext(ctx, `select r.id, r.title, u.display_name, r.created_at, r.expires_at, r.password_hash is not null,
		coalesce((select count(*) from room_online_members rom where rom.room_id = r.id and rom.connection_count > 0), 0),
		coalesce((select sum(size_bytes) from (
			select sf.sha256, sf.size_bytes from room_slides rs join slide_files sf on sf.sha256 = rs.sha256 where rs.room_id = r.id
			union
			select af.sha256, af.size_bytes from room_audio_tracks rat join audio_files af on af.sha256 = rat.sha256 where rat.room_id = r.id
		)), 0)
		from rooms r join users u on u.id = r.created_by_user_id`)
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}
	defer rows.Close()
	var rooms []cliRoom
	for rows.Next() {
		var room cliRoom
		if err := rows.Scan(&room.ID, &room.Title, &room.CreatorName, &room.CreatedAt, &room.ExpiresAt, &room.HasPassword, &room.OnlineCount, &room.SizeBytes); err != nil {
			return nil, fmt.Errorf("scan room: %w", err)
		}
		rooms = append(rooms, room)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate rooms: %w", err)
	}
	return rooms, nil
}

type removalPreview struct {
	RoomIDs       []string
	References    int
	Files         []string
	BytesFreed    int64
	SharedSkipped int
}

func runRemoveRoomFiles(cfg config.Config, args []string) error {
	flags := flag.NewFlagSet("rm", flag.ContinueOnError)
	yes := flags.Bool("y", false, "skip confirmation")
	if err := flags.Parse(args); err != nil {
		return err
	}
	roomIDs := flags.Args()
	if len(roomIDs) == 0 {
		return fmt.Errorf("usage: slidetalk rm ROOM_ID... [-y]")
	}
	db, err := openCLIStore(cfg)
	if err != nil {
		return err
	}
	defer db.Close()
	preview, err := previewRoomFileRemoval(context.Background(), db.DB, roomIDs)
	if err != nil {
		return err
	}
	fmt.Printf("Rooms: %s\n", strings.Join(preview.RoomIDs, ", "))
	fmt.Printf("Room file references removed: %d\n", preview.References)
	fmt.Printf("Files deleted: %d (%s freed)\n", len(preview.Files), formatBytes(preview.BytesFreed))
	fmt.Printf("Shared files kept: %d\n", preview.SharedSkipped)
	for _, path := range preview.Files {
		fmt.Printf("  delete %s\n", path)
	}
	if !*yes {
		fmt.Print("Expire these room files? Type yes to continue: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		if strings.TrimSpace(line) != "yes" {
			return nil
		}
	}
	return applyRoomFileRemoval(context.Background(), db.DB, preview)
}

func previewRoomFileRemoval(ctx context.Context, db *sql.DB, roomIDs []string) (removalPreview, error) {
	preview := removalPreview{RoomIDs: roomIDs}
	type fileRef struct {
		sha, path string
		size      int64
	}
	refs := map[string]fileRef{}
	for _, roomID := range roomIDs {
		fileRows, err := db.QueryContext(ctx, `select sf.sha256, sf.stored_path, sf.size_bytes from room_slides rs join slide_files sf on sf.sha256 = rs.sha256 where rs.room_id = ?
			union
			select af.sha256, af.stored_path, af.size_bytes from room_audio_tracks rat join audio_files af on af.sha256 = rat.sha256 where rat.room_id = ?`, roomID, roomID)
		if err != nil {
			return preview, fmt.Errorf("list room files: %w", err)
		}
		for fileRows.Next() {
			ref := fileRef{}
			if err := fileRows.Scan(&ref.sha, &ref.path, &ref.size); err != nil {
				_ = fileRows.Close()
				return preview, fmt.Errorf("scan room file: %w", err)
			}
			refs[ref.path] = ref
		}
		if err := fileRows.Close(); err != nil {
			return preview, err
		}
		var count int
		if err := db.QueryRowContext(ctx, `select (select count(*) from room_slides where room_id = ?) + (select count(*) from room_audio_tracks where room_id = ?)`, roomID, roomID).Scan(&count); err != nil {
			return preview, err
		}
		preview.References += count
	}
	for _, ref := range refs {
		var otherRefs int
		query := `select
			(select count(*) from room_slides where sha256 = ? and room_id not in (` + placeholders(len(roomIDs)) + `)) +
			(select count(*) from room_audio_tracks where sha256 = ? and room_id not in (` + placeholders(len(roomIDs)) + `))`
		queryArgs := []any{ref.sha}
		queryArgs = append(queryArgs, anyRoomIDs(roomIDs)...)
		queryArgs = append(queryArgs, ref.sha)
		queryArgs = append(queryArgs, anyRoomIDs(roomIDs)...)
		if err := db.QueryRowContext(ctx, query, queryArgs...).Scan(&otherRefs); err != nil {
			return preview, fmt.Errorf("count shared file refs: %w", err)
		}
		if otherRefs > 0 {
			preview.SharedSkipped++
			continue
		}
		preview.Files = append(preview.Files, ref.path)
		preview.BytesFreed += ref.size
	}
	sort.Strings(preview.Files)
	return preview, nil
}

func applyRoomFileRemoval(ctx context.Context, db *sql.DB, preview removalPreview) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, roomID := range preview.RoomIDs {
		if _, err := tx.ExecContext(ctx, `delete from audio_download_tokens where room_id = ?`, roomID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `delete from room_audio_track_stars where room_id = ?`, roomID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `delete from room_audio_tracks where room_id = ?`, roomID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `delete from room_slides where room_id = ?`, roomID); err != nil {
			return err
		}
	}
	for _, path := range preview.Files {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete %s: %w", path, err)
		}
		if _, err := tx.ExecContext(ctx, `delete from slide_files where stored_path = ? and not exists (select 1 from room_slides where sha256 = slide_files.sha256)`, path); err != nil {
			return err
		}
		var coverPath string
		_ = tx.QueryRowContext(ctx, `select cover_path from audio_files where stored_path = ?`, path).Scan(&coverPath)
		if coverPath != "" {
			if err := os.Remove(coverPath); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("delete %s: %w", coverPath, err)
			}
		}
		if _, err := tx.ExecContext(ctx, `delete from audio_files where stored_path = ? and not exists (select 1 from room_audio_tracks where sha256 = audio_files.sha256)`, path); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func openCLIStore(cfg config.Config) (*store.DB, error) {
	db, err := store.Open(cfg.DataDir)
	if err != nil {
		return nil, err
	}
	if err := store.Migrate(context.Background(), db.DB); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func cliPublicURL(cfg config.Config) string {
	if cfg.PublicURL != "" {
		return cfg.PublicURL
	}
	bytes, err := os.ReadFile(filepath.Join(cfg.DataDir, "public_url"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(bytes))
}

func truncate(value string, width int) string {
	if len(value) <= width {
		return value
	}
	if width <= 3 {
		return value[:width]
	}
	return value[:width-3] + "..."
}

func formatBytes(bytes int64) string {
	if bytes >= 1024*1024*1024 {
		return fmt.Sprintf("%.1fGB", float64(bytes)/1024/1024/1024)
	}
	if bytes >= 1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(bytes)/1024/1024)
	}
	if bytes >= 1024 {
		return fmt.Sprintf("%dKB", bytes/1024)
	}
	return fmt.Sprintf("%dB", bytes)
}

func placeholders(count int) string {
	if count <= 0 {
		return "''"
	}
	return strings.TrimRight(strings.Repeat("?,", count), ",")
}

func anyRoomIDs(roomIDs []string) []any {
	values := make([]any, len(roomIDs))
	for i, id := range roomIDs {
		values[i] = id
	}
	return values
}

func runSlideCleanup(ctx context.Context, service *slides.Service) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-ticker.C:
			if err := service.Cleanup(ctx, at.UTC()); err != nil {
				log.Printf("slide cleanup failed: %v", err)
			}
		}
	}
}

func runAudioCleanup(ctx context.Context, service *audio.Service, gcAfter time.Duration) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-ticker.C:
			if err := service.Cleanup(ctx, at.UTC(), gcAfter); err != nil {
				log.Printf("audio cleanup failed: %v", err)
			}
		}
	}
}

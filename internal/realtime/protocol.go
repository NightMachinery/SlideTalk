// Package realtime coordinates live room state over WebSockets.
package realtime

import (
	"encoding/json"
	"time"
)

const (
	EventSnapshot = "room.snapshot"
	EventKicked   = "room.kicked"
	EventError    = "error"

	CommandPeopleReorder          = "people.reorder"
	CommandPeopleSetRole          = "people.setRole"
	CommandPeopleAudioPermission  = "people.audioPermission"
	CommandPeopleTagPermission    = "people.tagPermission"
	CommandPeopleKick             = "people.kick"
	CommandTurnNext               = "turn.next"
	CommandTurnPrevious           = "turn.previous"
	CommandTurnSetCurrent         = "turn.setCurrent"
	CommandTimerStart             = "timer.start"
	CommandTimerStop              = "timer.stop"
	CommandTimerReset             = "timer.reset"
	CommandHandRaise              = "hand.raise"
	CommandHandLower              = "hand.lower"
	CommandSlideNavigate          = "slide.navigate"
	CommandMarkdownUpdate         = "markdown.update"
	CommandSettingsUpdate         = "settings.update"
	CommandAudioPlay              = "audio.play"
	CommandAudioPause             = "audio.pause"
	CommandAudioSeek              = "audio.seek"
	CommandAudioSelect            = "audio.select"
	CommandAudioReorder           = "audio.reorder"
	CommandAudioMode              = "audio.mode"
	CommandAudioEnded             = "audio.ended"
	CommandAudioStar              = "audio.star"
	CommandAudioTag               = "audio.tag"
	CommandAudioFilterScope       = "audio.filterScope"
	CommandPresenceAudioLocalMode = "presence.audioLocalMode"
)

const (
	TimerStateStopped = "stopped"
	TimerStateRunning = "running"

	AudioStatePaused  = "paused"
	AudioStatePlaying = "playing"

	AudioModeStop           = "stop"
	AudioModeNext           = "next"
	AudioModePrevious       = "previous"
	AudioModeRepeatOne      = "repeat-one"
	AudioModeRepeatForward  = "repeat-forward"
	AudioModeRepeatBackward = "repeat-backward"
	AudioModeShuffle        = "shuffle"

	RoomModeSlides   = "slides"
	RoomModeMarkdown = "markdown"
	RoomModeAudio    = "audio"

	RaiseHandModeOff    = "off"
	RaiseHandModeManual = "manual"
	RaiseHandModeQueue  = "queue"
)

// Command is the client-to-server realtime envelope.
type Command struct {
	Type      string          `json:"type"`
	RequestID string          `json:"requestId"`
	Payload   json.RawMessage `json:"payload"`
}

// Event is the server-to-client realtime envelope.
type Event struct {
	Type      string `json:"type"`
	RequestID string `json:"requestId,omitempty"`
	RoomID    string `json:"roomId,omitempty"`
	Version   int64  `json:"version,omitempty"`
	Code      string `json:"code,omitempty"`
	Message   string `json:"message,omitempty"`
	Payload   any    `json:"payload,omitempty"`
}

// Snapshot is the full room state sent to a connected client.
type Snapshot struct {
	Room                    SnapshotRoom     `json:"room"`
	Caller                  SnapshotCaller   `json:"caller"`
	Participants            []SnapshotMember `json:"participants"`
	Observers               []SnapshotMember `json:"observers"`
	CurrentTurn             SnapshotTurn     `json:"currentTurn"`
	Timer                   SnapshotTimer    `json:"timer"`
	Hands                   []SnapshotHand   `json:"hands"`
	Slide                   *SnapshotSlide   `json:"slide"`
	Markdown                string           `json:"markdown"`
	MarkdownUpdatedByUserID string           `json:"markdownUpdatedByUserId"`
	MarkdownUpdatedByName   string           `json:"markdownUpdatedByName"`
	MarkdownUpdatedAt       string           `json:"markdownUpdatedAt"`
	Audio                   SnapshotAudio    `json:"audio"`
}

// SnapshotRoom is room metadata in realtime snapshots.
type SnapshotRoom struct {
	ID                        string `json:"id"`
	Title                     string `json:"title"`
	HasPassword               bool   `json:"hasPassword"`
	AllowParticipantMarkdown  bool   `json:"allowParticipantMarkdown"`
	RaiseHandMode             string `json:"raiseHandMode"`
	SlidePage                 int    `json:"slidePage"`
	SharedNavigationEnabled   bool   `json:"sharedNavigationEnabled"`
	RoomMode                  string `json:"roomMode"`
	AllowAudienceAudioUpload  bool   `json:"allowAudienceAudioUpload"`
	AllowAudienceAudioControl bool   `json:"allowAudienceAudioControl"`
	ShowAudioStarCounts       bool   `json:"showAudioStarCounts"`
	AllowAudienceAudioTagging bool   `json:"allowAudienceAudioTagging"`
	ExpiresAt                 string `json:"expiresAt"`
	NeverExpires              bool   `json:"neverExpires"`
}

// SnapshotCaller identifies the receiving user.
type SnapshotCaller struct {
	UserID  string `json:"userId"`
	Role    string `json:"role"`
	IsAdmin bool   `json:"isAdmin"`
}

// SnapshotMember is a participant or observer row.
type SnapshotMember struct {
	UserID            string `json:"userId"`
	DisplayName       string `json:"displayName"`
	Role              string `json:"role"`
	DisplayOrder      int    `json:"displayOrder"`
	IsOnline          bool   `json:"isOnline"`
	AllowAudioUpload  bool   `json:"allowAudioUpload"`
	AllowAudioControl bool   `json:"allowAudioControl"`
	AllowAudioTagging bool   `json:"allowAudioTagging"`
	AudioLocalMode    bool   `json:"audioLocalMode"`
}

// SnapshotTurn describes current and next speakers.
type SnapshotTurn struct {
	CurrentSpeakerUserID string `json:"currentSpeakerUserId"`
	NextSpeakerUserID    string `json:"nextSpeakerUserId"`
}

// SnapshotTimer contains enough server timing for clients to render a countdown.
type SnapshotTimer struct {
	State           string  `json:"state"`
	DurationSeconds int     `json:"durationSeconds"`
	StartedAt       *string `json:"startedAt"`
	ServerNow       string  `json:"serverNow"`
}

// SnapshotHand is one raised hand in queue order.
type SnapshotHand struct {
	UserID      string `json:"userId"`
	DisplayName string `json:"displayName"`
	RaisedAt    string `json:"raisedAt"`
}

// SnapshotSlide describes the room's attached deck.
type SnapshotSlide struct {
	SHA256       string `json:"sha256"`
	OriginalName string `json:"originalName"`
	MIMEType     string `json:"mimeType"`
	ExpiresAt    string `json:"expiresAt"`
	Missing      bool   `json:"missing"`
}

// SnapshotAudio contains shared room audio playlist and playback state.
type SnapshotAudio struct {
	Tracks          []SnapshotAudioTrack     `json:"tracks"`
	CurrentTrackID  string                   `json:"currentTrackId"`
	NextTrackID     string                   `json:"nextTrackId"`
	State           string                   `json:"state"`
	PositionSeconds int                      `json:"positionSeconds"`
	StartedAt       *string                  `json:"startedAt"`
	ServerNow       string                   `json:"serverNow"`
	PlaybackMode    string                   `json:"playbackMode"`
	FilterScope     SnapshotAudioFilterScope `json:"filterScope"`
}

type SnapshotAudioFilterGroup struct {
	Tags []string `json:"tags"`
}

// SnapshotAudioFilterScope is the published synced audio browsing and playback scope.
type SnapshotAudioFilterScope struct {
	Search          string                     `json:"search"`
	IncludeGroups   []SnapshotAudioFilterGroup `json:"includeGroups"`
	ExcludeTags     []string                   `json:"excludeTags"`
	StarredOnly     bool                       `json:"starredOnly"`
	UpdatedByUserID string                     `json:"updatedByUserId"`
	UpdatedAt       string                     `json:"updatedAt"`
}

// SnapshotAudioTagClaim describes one user/source claim on an audio tag.
type SnapshotAudioTagClaim struct {
	UserID string `json:"userId"`
	Source string `json:"source"`
}

// SnapshotAudioTag describes one displayed tag and its layered claims.
type SnapshotAudioTag struct {
	Slug   string                  `json:"slug"`
	Label  string                  `json:"label"`
	Claims []SnapshotAudioTagClaim `json:"claims"`
}

// SnapshotAudioTrack describes one shared audio track.
type SnapshotAudioTrack struct {
	ID                  string             `json:"id"`
	SHA256              string             `json:"sha256"`
	OriginalName        string             `json:"originalName"`
	Title               string             `json:"title"`
	MetadataTitle       string             `json:"metadataTitle"`
	MIMEType            string             `json:"mimeType"`
	SizeBytes           int64              `json:"sizeBytes"`
	DurationSeconds     int                `json:"durationSeconds"`
	HasCover            bool               `json:"hasCover"`
	UploadedByUserID    string             `json:"uploadedByUserId"`
	UploadedByName      string             `json:"uploadedByName"`
	UploaderDisplayName string             `json:"uploaderDisplayName"`
	DisplayOrder        int                `json:"displayOrder"`
	Missing             bool               `json:"missing"`
	StarredByCaller     bool               `json:"starredByCaller"`
	StarCount           int                `json:"starCount,omitempty"`
	Tags                []SnapshotAudioTag `json:"tags"`
}

// WSTicket is a short-lived room-scoped connection token.
type WSTicket struct {
	Ticket    string    `json:"ticket"`
	ExpiresAt time.Time `json:"expiresAt"`
}

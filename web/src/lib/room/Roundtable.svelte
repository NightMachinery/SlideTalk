<script lang="ts">
  import { onMount } from 'svelte';
  import ArrowDown from '@lucide/svelte/icons/arrow-down';
  import ArrowUp from '@lucide/svelte/icons/arrow-up';
  import ChevronLeft from '@lucide/svelte/icons/chevron-left';
  import ChevronRight from '@lucide/svelte/icons/chevron-right';
  import ChevronsLeft from '@lucide/svelte/icons/chevrons-left';
  import ChevronsRight from '@lucide/svelte/icons/chevrons-right';
  import Copy from '@lucide/svelte/icons/copy';
  import Crosshair from '@lucide/svelte/icons/crosshair';
  import Eye from '@lucide/svelte/icons/eye';
  import FileText from '@lucide/svelte/icons/file-text';
  import FileWarning from '@lucide/svelte/icons/file-warning';
  import Hand from '@lucide/svelte/icons/hand';
  import HardDrive from '@lucide/svelte/icons/hard-drive';
  import Link2 from '@lucide/svelte/icons/link-2';
  import LogOut from '@lucide/svelte/icons/log-out';
  import Download from '@lucide/svelte/icons/download';
  import Pencil from '@lucide/svelte/icons/pencil';
  import Mic from '@lucide/svelte/icons/mic';
  import Music from '@lucide/svelte/icons/music';
  import UserX from '@lucide/svelte/icons/user-x';
  import Volume2 from '@lucide/svelte/icons/volume-2';
  import VolumeX from '@lucide/svelte/icons/volume-x';
  import Pause from '@lucide/svelte/icons/pause';
  import Play from '@lucide/svelte/icons/play';
  import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
  import Save from '@lucide/svelte/icons/save';
  import Settings from '@lucide/svelte/icons/settings';
  import SkipBack from '@lucide/svelte/icons/skip-back';
  import SkipForward from '@lucide/svelte/icons/skip-forward';
  import Search from '@lucide/svelte/icons/search';
  import SlidersHorizontal from '@lucide/svelte/icons/sliders-horizontal';
  import Shield from '@lucide/svelte/icons/shield';
  import Star from '@lucide/svelte/icons/star';
  import Timer from '@lucide/svelte/icons/timer';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import Upload from '@lucide/svelte/icons/upload';
  import UserRound from '@lucide/svelte/icons/user-round';
  import UsersRound from '@lucide/svelte/icons/users-round';
  import { audioCoverRequest, audioFileRequest, checkUploadPreflight, createAudioDownloadLink, createMigrationLink, getSlideStatus, insufficientFreeSpaceMessage, removeRoomAudio, removeRoomSlide, slideFileRequest, updateRoomAudio, updateRoomSettings, updateRoomSlideExpiration, uploadRoomAudio, uploadRoomSlide } from '../api';
  import { audioCacheStats, audioSubtype, clearAudioCache, fileNameWithoutExtension, gcAudioCache, getCachedAudio, hiddenUploaderDisplayName, listCachedAudio, putCachedAudio, trackDisplayTitle, trackUploaderName } from '../audioCache';
  import { readAudioUploadMetadata } from '../audioMetadata';
  import { cacheLimits } from '../cacheConstants';
  import { copyText } from '../clipboard';
  import type { CacheStats } from '../blobCache';
  import type { RealtimeCommand, RoomSnapshot, SnapshotMember } from '../realtime';
  import { clearSlideCache, gcSlideCache, getCachedSlide, putCachedSlide, slideCacheStats } from '../slideCache';
  import { addToast } from '../toast.svelte';
  import { classifyAudioUploadFiles, safeBrowserAudio } from './audioUploadValidation';
  import { loadLocalAudioPreferences, saveLocalAudioPreferences } from './localAudioPreferences';
  import { nextLocalAudioTrackId, type AudioPlaybackMode } from './localAudioMode';
  import { displayNameForRoom, onlineCountLabel, sortedByOnline } from './memberDisplay';
  import { parseMarkdown } from './markdown';
  import SelectMenu from './SelectMenu.svelte';
  import { pageFromSharedNavigation } from './slides';
  import {
    defaultShortcutBindings,
    formatKey,
    loadShortcutConfig,
    resetShortcutBinding,
    resolveShortcutAction,
    saveShortcutConfig,
    setShortcutBinding,
    shortcutLabels,
    shouldIgnoreShortcut,
    type RebindableShortcutAction,
    type ShortcutConfig
  } from './shortcuts';

  type PDFDocumentLike = {
    numPages: number;
    getPage(page: number): Promise<{
      getViewport(input: { scale: number }): { width: number; height: number };
      render(input: { canvasContext: CanvasRenderingContext2D; viewport: { width: number; height: number } }): {
        promise: Promise<void>;
      };
    }>;
  };

  type PanelState = {
    railCollapsed: boolean;
    participants: boolean;
    observers: boolean;
    slides: boolean;
    audio: boolean;
    settings: boolean;
    shortcuts: boolean;
    cache: boolean;
  };

  const panelStorageKey = 'slidetalk.roomPanels.v1';
  const shortcutActions: RebindableShortcutAction[] = ['previousSpeaker', 'nextSpeaker', 'toggleTimerPause', 'resetAndStartTimer', 'previousSlide', 'nextSlide'];
  const roomModeOptions = [
    { value: 'slides', label: 'Slides' },
    { value: 'markdown', label: 'Markdown' },
    { value: 'audio', label: 'Audio' }
  ];
  const finishModeOptions = [
    { value: 'stop', label: 'Stop' },
    { value: 'next', label: 'Next' },
    { value: 'previous', label: 'Previous' },
    { value: 'repeat-one', label: 'Repeat one' },
    { value: 'repeat-forward', label: 'Repeat Forward' },
    { value: 'repeat-backward', label: 'Repeat Backward' },
    { value: 'shuffle', label: 'Shuffle' }
  ];
  const handModeOptions = [
    { value: 'off', label: 'Off' },
    { value: 'manual', label: 'Manual' },
    { value: 'queue', label: 'Queue' }
  ];

  let {
    snapshot,
    status,
    audioDriftThresholdSeconds = 3,
    send
  }: {
    snapshot: RoomSnapshot;
    status: 'connecting' | 'connected' | 'disconnected';
    audioDriftThresholdSeconds?: number;
    send: (command: RealtimeCommand, options?: { notifyOnFailure?: boolean }) => void;
  } = $props();

  let panelState = $state<PanelState>({
    railCollapsed: false,
    participants: true,
    observers: true,
    slides: true,
    audio: true,
    settings: true,
    shortcuts: true,
    cache: true
  });
  let shortcutConfig = $state<ShortcutConfig>(loadShortcutConfig(null));
  let shortcutDrafts = $state<Record<RebindableShortcutAction, string>>({ ...defaultShortcutBindings });
  let preferencesReady = $state(false);
  let timerDurationSeconds = $state(300);
  let previousRemaining = $state(-1);
  let timerEndedPulse = $state(false);
  let nowMs = $state(Date.now());
  let slideFile = $state<File | null>(null);
  let slideExpiresAt = $state(defaultExpirationInput());
  let slideBusy = $state(false);
  let slideProgress = $state(0);
  let slideMessage = $state('');
  let slideConfirmRemove = $state(false);
  let slideCanvas = $state<HTMLCanvasElement | null>(null);
  let pdfDocument = $state<PDFDocumentLike | null>(null);
  let imageObjectUrl = $state('');
  let activeImageObjectUrl = '';
  let localPage = $state(1);
  let stageResizeTick = $state(0);
  let followSharedNavigation = $state(true);
  let modShareNavigation = $state(false);
  let markdownDraft = $state('');
  let markdownMessage = $state('');
  let roomTitleDraft = $state('');
  let roomPasswordDraft = $state('');
  let settingsMessage = $state('');
  let migrationFallbackText = $state('');
  let audioFiles = $state<File[]>([]);
  let audioElement = $state<HTMLAudioElement | null>(null);
  let audioObjectUrl = $state('');
  let activeAudioObjectUrl = '';
  let activeAudioTrackId = '';
  let activeAudioSourceKey = '';
  let audioBusy = $state(false);
  let audioProgress = $state(0);
  let audioUploadIndex = $state(0);
  let audioWorkTotal = $state(0);
  let audioMessage = $state('');
  let audioBlocked = $state(false);
  let audioMuted = $state(false);
  let audioVolume = $state(1);
  let localVolumeOpen = $state(false);
  let localVolumeControlElement = $state<HTMLElement | null>(null);
  let finishModeOpen = $state(false);
  let finishModeControlElement = $state<HTMLElement | null>(null);
  let localVolumeLongPress: number | null = null;
  let localVolumeSuppressClick = false;
  let localVolumeDragging = false;
  let localVolumePointerId: number | null = null;
  let localVolumePointerTarget: HTMLElement | null = null;
  let localVolumeDragStartY = 0;
  let localVolumeDragStartVolume = 1;
  let audioPreferencesKey = '';
  let audioPositionDraft = $state(0);
  let audioSeeking = $state(false);
  let audioDuration = $state(0);
  let audioBufferedPercent = $state(0);
  let audioDownloadProgress = $state<Record<string, number>>({});
  let audioDownloadBusy = $state<Record<string, boolean>>({});
  let audioSearchQuery = $state('');
  let audioDownloaded = $state<Record<string, boolean>>({});
  let activeNextAudioCacheKey = '';
  let audioCoverUrls = $state<Record<string, string>>({});
  let activeAudioCoverObjectUrl = '';
  let editingAudioTrackId = $state('');
  let audioTitleDraft = $state('');
  let audioUploaderDraft = $state('');
  let audioCacheUsage = $state<CacheStats>({ entries: 0, bytes: 0 });
  let manualLocalAudioMode = $state(false);
  let offlineLocalAudioMode = $state(false);
  let localAudioTrackId = $state('');
  let localAudioState = $state<RoomSnapshot['audio']['state']>('paused');
  let localAudioPositionSeconds = $state(0);
  let localAudioStartedAtMs = 0;
  let localAudioPlaybackMode = $state<AudioPlaybackMode>('stop');
  let publishedManualLocalAudioMode: boolean | null = null;
  let audioTrackElements = new Map<string, HTMLElement>();
  let audioRealtimeHasConnected = $state(false);
  let slideCacheUsage = $state<CacheStats>({ entries: 0, bytes: 0 });
  let starredOnly = $state(false);
  let retentionDraft = $state('');
  let cacheBusy = $state(false);
  let cacheMessage = $state('');
  let confirmDialog = $state<{
    open: boolean;
    accent: 'danger' | 'warning';
    title: string;
    message: string;
    confirmLabel: string;
    onConfirm: () => void | Promise<void>;
  }>({ open: false, accent: 'danger', title: '', message: '', confirmLabel: 'Confirm', onConfirm: () => {} });
  let pendingConfirmResolve: ((confirmed: boolean) => void) | null = null;

  const isMod = $derived(snapshot.caller.role === 'mod');
  const canManageSlides = $derived(isMod);
  const canChangeSlideExpiration = $derived(isMod && snapshot.caller.isAdmin);
  const canManageRetention = $derived(isMod || snapshot.caller.isAdmin);
  const canEditMarkdown = $derived(isMod || (snapshot.caller.role === 'participant' && snapshot.room.allowParticipantMarkdown));
  const currentSpeaker = $derived(snapshot.participants.find((member) => member.userId === snapshot.currentTurn.currentSpeakerUserId));
  const nextSpeaker = $derived(snapshot.participants.find((member) => member.userId === snapshot.currentTurn.nextSpeakerUserId));
  const displayedParticipants = $derived(sortedByOnline(snapshot.participants));
  const displayedObservers = $derived(sortedByOnline(snapshot.observers));
  const allRoomMembers = $derived([...snapshot.participants, ...snapshot.observers]);
  const participantCountLabel = $derived(onlineCountLabel(snapshot.participants));
  const observerCountLabel = $derived(onlineCountLabel(snapshot.observers));
  const callerParticipant = $derived(snapshot.participants.find((member) => member.userId === snapshot.caller.userId));
  const callerHand = $derived(snapshot.hands.find((hand) => hand.userId === snapshot.caller.userId));
  const canUseHands = $derived(snapshot.caller.role !== 'observer' && snapshot.room.raiseHandMode !== 'off');
  const markdownBlocks = $derived(parseMarkdown(snapshot.markdown || ''));
  const markdownEditorVisible = $derived(snapshot.room.roomMode === 'markdown' && canEditMarkdown);
  const canSeeAudio = true;
  const canUploadAudio = $derived(isMod || (snapshot.caller.role === 'participant' && (snapshot.room.allowAudienceAudioUpload || !!callerParticipant?.allowAudioUpload)));
  const canControlAudio = $derived(isMod || (snapshot.caller.role === 'participant' && (snapshot.room.allowAudienceAudioControl || !!callerParticipant?.allowAudioControl)));
  const localAudioModeActive = $derived(manualLocalAudioMode || offlineLocalAudioMode);
  const canUseAudioControls = $derived(canControlAudio || localAudioModeActive);
  const localEstimatedAudioSeconds = $derived(localAudioState === 'playing' ? Math.max(Math.floor((nowMs - localAudioStartedAtMs) / 1000), 0) : localAudioPositionSeconds);
  const effectiveCurrentAudioTrackId = $derived(localAudioModeActive ? localAudioTrackId : snapshot.audio.currentTrackId);
  const effectiveAudioState = $derived(localAudioModeActive ? localAudioState : snapshot.audio.state);
  const effectiveAudioPositionSeconds = $derived(localAudioModeActive ? localEstimatedAudioSeconds : estimatedAudioSeconds);
  const effectiveAudioPlaybackMode = $derived(localAudioModeActive ? localAudioPlaybackMode : snapshot.audio.playbackMode);
  const currentAudioTrack = $derived(snapshot.audio.tracks.find((track) => track.id === effectiveCurrentAudioTrackId) ?? snapshot.audio.tracks[0]);
  const normalizedAudioSearchQuery = $derived(audioSearchQuery.trim().toLowerCase());
  const displayedAudioTracks = $derived.by(() => {
    const tracks = starredOnly ? snapshot.audio.tracks.filter((track) => track.starredByCaller) : snapshot.audio.tracks;
    if (!normalizedAudioSearchQuery) return tracks;
    return tracks.filter((track) => audioTrackMatchesSearch(track, normalizedAudioSearchQuery));
  });
  const slideMimeType = $derived(snapshot.slide?.mimeType || 'application/pdf');
  const slideIsPDF = $derived(slideMimeType === 'application/pdf');
  const slideIsImage = $derived(slideMimeType.startsWith('image/'));
  const timerSync = $derived.by(() => {
    const serverNowMs = Date.parse(snapshot.timer.serverNow);
    const receivedAtMs = Date.now();
    return {
      receivedAtMs,
      serverNowMs: Number.isNaN(serverNowMs) ? receivedAtMs : serverNowMs
    };
  });
  const remainingSeconds = $derived.by(() => {
    if (snapshot.timer.state !== 'running' || !snapshot.timer.startedAt) return snapshot.timer.durationSeconds;
    const startedAt = Date.parse(snapshot.timer.startedAt);
    if (Number.isNaN(startedAt)) return snapshot.timer.durationSeconds;
    const estimatedServerNow = timerSync.serverNowMs + (nowMs - timerSync.receivedAtMs);
    const elapsed = Math.floor((estimatedServerNow - startedAt) / 1000);
    return Math.max(snapshot.timer.durationSeconds - elapsed, 0);
  });
  const timerLabel = $derived(formatDuration(remainingSeconds));
  const audioSync = $derived.by(() => {
    const serverNowMs = Date.parse(snapshot.audio.serverNow);
    const receivedAtMs = Date.now();
    return {
      receivedAtMs,
      serverNowMs: Number.isNaN(serverNowMs) ? receivedAtMs : serverNowMs
    };
  });
  const estimatedAudioSeconds = $derived.by(() => {
    if (snapshot.audio.state !== 'playing' || !snapshot.audio.startedAt) return snapshot.audio.positionSeconds;
    const startedAt = Date.parse(snapshot.audio.startedAt);
    if (Number.isNaN(startedAt)) return snapshot.audio.positionSeconds;
    const estimatedServerNow = audioSync.serverNowMs + (nowMs - audioSync.receivedAtMs);
    return Math.max(snapshot.audio.positionSeconds + Math.floor((estimatedServerNow - startedAt) / 1000), 0);
  });

  onMount(() => {
    panelState = loadPanelState();
    shortcutConfig = loadShortcutConfig();
    shortcutDrafts = { ...shortcutConfig.bindings };
    preferencesReady = true;
    const interval = window.setInterval(() => {
      nowMs = Date.now();
    }, 1000);
    void Promise.all([gcAudioCache(), gcSlideCache()])
      .then(() => refreshCacheUsage())
      .catch(() => {});
    const resize = () => {
      stageResizeTick += 1;
    };
    window.addEventListener('resize', resize);
    return () => {
      window.clearInterval(interval);
      window.removeEventListener('resize', resize);
    };
  });

  $effect(() => {
    const roomId = snapshot.room.id;
    const userId = snapshot.caller.userId;
    const preferencesKey = `${roomId}:${userId}`;
    if (audioPreferencesKey === preferencesKey) return;
    audioPreferencesKey = preferencesKey;
    const preferences = loadLocalAudioPreferences(roomId, userId);
    audioMuted = preferences.muted;
    audioVolume = preferences.volume;
  });

  $effect(() => {
    if (audioElement) {
      audioElement.muted = audioMuted;
      audioElement.volume = audioVolume;
    }
  });

  $effect(() => {
    if (status === 'connected') {
      audioRealtimeHasConnected = true;
      offlineLocalAudioMode = false;
      return;
    }
    if (!audioRealtimeHasConnected) return;
    const timer = window.setTimeout(() => {
      if (!usingLocalAudioMode()) seedLocalAudioState();
      offlineLocalAudioMode = true;
    }, 1500);
    return () => window.clearTimeout(timer);
  });

  $effect(() => {
    if (status !== 'connected') {
      publishedManualLocalAudioMode = null;
      return;
    }
    if (publishedManualLocalAudioMode === manualLocalAudioMode) return;
    publishedManualLocalAudioMode = manualLocalAudioMode;
    send({ type: 'presence.audioLocalMode', payload: { enabled: manualLocalAudioMode } }, { notifyOnFailure: false });
  });

  $effect(() => {
    if (typeof navigator === 'undefined' || !('mediaSession' in navigator)) return;
    const mediaSession = navigator.mediaSession;
    try {
      mediaSession.setActionHandler('play', () => handleMediaPlaybackAction('play'));
      mediaSession.setActionHandler('pause', () => handleMediaPlaybackAction('pause'));
      mediaSession.setActionHandler('stop', () => handleMediaPlaybackAction('stop'));
      mediaSession.setActionHandler('previoustrack', () => handleMediaPlaybackAction('previous'));
      mediaSession.setActionHandler('nexttrack', () => handleMediaPlaybackAction('next'));
      mediaSession.setActionHandler('seekbackward', () => handleMediaPlaybackAction('seek-backward'));
      mediaSession.setActionHandler('seekforward', () => handleMediaPlaybackAction('seek-forward'));
      mediaSession.setActionHandler('seekto', (details) => {
        if (typeof details.seekTime === 'number') seekAudio(details.seekTime);
      });
    } catch {
      // Older browsers can reject unsupported media session actions.
    }
    return () => {
      try {
        mediaSession.setActionHandler('play', null);
        mediaSession.setActionHandler('pause', null);
        mediaSession.setActionHandler('stop', null);
        mediaSession.setActionHandler('previoustrack', null);
        mediaSession.setActionHandler('nexttrack', null);
        mediaSession.setActionHandler('seekbackward', null);
        mediaSession.setActionHandler('seekforward', null);
        mediaSession.setActionHandler('seekto', null);
      } catch {
        // Ignore cleanup errors from partial implementations.
      }
    };
  });

  $effect(() => {
    if (!preferencesReady) return;
    savePanelState(panelState);
  });

  $effect(() => {
    if (!preferencesReady) return;
    saveShortcutConfig(shortcutConfig);
  });

  $effect(() => {
    if (followSharedNavigation) {
      localPage = pageFromSharedNavigation({
        localPage,
        sharedPage: snapshot.room.slidePage,
        followShared: followSharedNavigation
      });
    }
    modShareNavigation = snapshot.room.sharedNavigationEnabled;
  });

  $effect(() => {
    markdownDraft = snapshot.markdown;
  });

  $effect(() => {
    roomTitleDraft = snapshot.room.title;
  });

  $effect(() => {
    if (snapshot.timer.state === 'running' && previousRemaining > 0 && remainingSeconds === 0) {
      playBeep();
      timerEndedPulse = true;
      window.setTimeout(() => {
        timerEndedPulse = false;
      }, 2000);
    }
    previousRemaining = remainingSeconds;
  });

  $effect(() => {
    if (snapshot.slide?.expiresAt) {
      slideExpiresAt = new Date(snapshot.slide.expiresAt).toISOString().slice(0, 16);
    }
  });

  $effect(() => {
    retentionDraft = snapshot.room.expiresAt ? new Date(snapshot.room.expiresAt).toISOString().slice(0, 16) : '';
  });

  $effect(() => {
    const slideKey = snapshot.slide?.sha256;
    if (!slideKey || snapshot.slide?.missing || snapshot.room.roomMode !== 'slides' || !slideIsPDF) {
      pdfDocument = null;
      return;
    }

    let cancelled = false;
    void loadPDF().catch((error) => {
      if (!cancelled) {
        pdfDocument = null;
        addToast(errorMessage(error, 'Could not load PDF.'));
      }
    });

    async function loadPDF() {
      const [{ GlobalWorkerOptions, getDocument }, worker] = await Promise.all([
        import('pdfjs-dist'),
        import('pdfjs-dist/build/pdf.worker.mjs?url')
      ]);
      GlobalWorkerOptions.workerSrc = worker.default;
      const cached = await getCachedSlide(slideKey);
      let blob = cached?.blob;
      if (!blob) {
        const request = slideFileRequest(snapshot.room.id);
        const response = await fetch(request.url, { headers: request.headers });
        if (!response.ok) throw new Error('Could not load PDF.');
        blob = await response.blob();
        void putCachedSlide({
          sha256: slideKey,
          blob,
          mimeType: snapshot.slide?.mimeType ?? blob.type,
          originalName: snapshot.slide?.originalName ?? 'slide.pdf',
          sizeBytes: blob.size
        }).then(() => refreshCacheUsage()).catch(() => {});
      }
      const document = await getDocument({ data: new Uint8Array(await blob.arrayBuffer()) }).promise;
      if (!cancelled) {
        pdfDocument = document as PDFDocumentLike;
        localPage = pageFromSharedNavigation({
          localPage,
          sharedPage: snapshot.room.slidePage,
          followShared: followSharedNavigation
        });
      }
    }

    return () => {
      cancelled = true;
    };
  });

  $effect(() => {
    const trackID = effectiveCurrentAudioTrackId;
    if (!trackID || !canSeeAudio) {
      activeAudioSourceKey = '';
      if (activeAudioObjectUrl) {
        URL.revokeObjectURL(activeAudioObjectUrl);
        activeAudioObjectUrl = '';
        audioObjectUrl = '';
      }
      activeAudioTrackId = '';
      return;
    }
    const track = snapshot.audio.tracks.find((item) => item.id === trackID);
    const sourceKey = `${snapshot.room.id}:${trackID}:${track?.sha256 ?? ''}:${track?.missing ?? false}`;
    if (sourceKey === activeAudioSourceKey) return;
    activeAudioSourceKey = sourceKey;

    let cancelled = false;
    let nextBlobUrl = '';
    activeAudioTrackId = trackID;
    audioBufferedPercent = 0;
    void loadAudioSource().catch((error) => {
      if (!cancelled) {
        addToast(errorMessage(error, 'Could not load audio.'));
      }
    });

    async function loadAudioSource() {
      if (!track) return;
      const cached = await getCachedAudio(track.sha256);
      if (cancelled || activeAudioTrackId !== trackID) return;
      if (cached) {
        audioDownloaded = { ...audioDownloaded, [track.sha256]: true };
        nextBlobUrl = URL.createObjectURL(cached.blob);
        setAudioSource(trackID, nextBlobUrl, true);
        return;
      }

      const link = await createAudioDownloadLink(snapshot.room.id, trackID);
      if (cancelled || activeAudioTrackId !== trackID) return;
      setAudioSource(trackID, link.url, false);
      void downloadAndCacheAudio(track, link.url);
    }

    return () => {
      cancelled = true;
      if (nextBlobUrl && nextBlobUrl !== activeAudioObjectUrl) URL.revokeObjectURL(nextBlobUrl);
    };
  });

  $effect(() => {
    const nextTrackID = snapshot.audio.nextTrackId;
    if (!canSeeAudio || !nextTrackID || nextTrackID === effectiveCurrentAudioTrackId) {
      activeNextAudioCacheKey = '';
      return;
    }
    const track = snapshot.audio.tracks.find((item) => item.id === nextTrackID);
    if (!track || track.missing) {
      activeNextAudioCacheKey = '';
      return;
    }
    const cacheKey = `${snapshot.room.id}:${nextTrackID}:${track.sha256}`;
    if (cacheKey === activeNextAudioCacheKey) return;
    activeNextAudioCacheKey = cacheKey;
    let cancelled = false;
    void cacheNextAudioTrack().catch(() => {});

    async function cacheNextAudioTrack() {
      const cached = await getCachedAudio(track.sha256);
      if (cancelled || activeNextAudioCacheKey !== cacheKey) return;
      if (cached) {
        audioDownloaded = { ...audioDownloaded, [track.sha256]: true };
        audioDownloadProgress = { ...audioDownloadProgress, [track.sha256]: 100 };
        return;
      }
      const link = await createAudioDownloadLink(snapshot.room.id, nextTrackID);
      if (cancelled || activeNextAudioCacheKey !== cacheKey) return;
      await downloadAndCacheAudio(track, link.url);
    }

    return () => {
      cancelled = true;
    };
  });

  $effect(() => {
    if (!audioElement || !audioObjectUrl) return;
    const desired = effectiveAudioPositionSeconds;
    if (Number.isFinite(desired) && Math.abs(audioElement.currentTime - desired) > audioDriftThresholdSeconds) {
      audioElement.currentTime = desired;
    }
    if (!audioSeeking) audioPositionDraft = Math.floor(audioElement.currentTime || desired);
    if (effectiveAudioState === 'playing') {
      void audioElement.play().then(() => {
        audioBlocked = false;
      }).catch((error) => {
        if (error.name !== 'AbortError') audioBlocked = true;
      });
    } else if (!audioElement.paused) {
      audioElement.pause();
    }
  });

  $effect(() => {
    const track = currentAudioTrack;
    if (!track?.hasCover || !canSeeAudio) {
      if (activeAudioCoverObjectUrl) URL.revokeObjectURL(activeAudioCoverObjectUrl);
      activeAudioCoverObjectUrl = '';
      return;
    }
    let cancelled = false;
    const request = audioCoverRequest(snapshot.room.id, track.id);
    void fetch(request.url, { headers: request.headers })
      .then((response) => {
        if (!response.ok) throw new Error('cover');
        return response.blob();
      })
      .then((blob) => {
        if (cancelled) return;
        if (activeAudioCoverObjectUrl) URL.revokeObjectURL(activeAudioCoverObjectUrl);
        activeAudioCoverObjectUrl = URL.createObjectURL(blob);
        audioCoverUrls = { ...audioCoverUrls, [track.id]: activeAudioCoverObjectUrl };
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  });

  $effect(() => {
    const slideKey = snapshot.slide?.sha256;
    if (!slideKey || snapshot.slide?.missing || snapshot.room.roomMode !== 'slides' || !slideIsImage) {
      if (activeImageObjectUrl) {
        URL.revokeObjectURL(activeImageObjectUrl);
        activeImageObjectUrl = '';
        imageObjectUrl = '';
      }
      return;
    }

    let cancelled = false;
    let nextUrl = '';
    void loadImage().catch((error) => {
      if (!cancelled) {
        addToast(errorMessage(error, 'Could not load image slide.'));
      }
    });

    async function loadImage() {
      const cached = await getCachedSlide(slideKey);
      let blob = cached?.blob;
      if (!blob) {
        const request = slideFileRequest(snapshot.room.id);
        const response = await fetch(request.url, { headers: request.headers });
        if (!response.ok) throw new Error('Could not load image slide.');
        blob = await response.blob();
        void putCachedSlide({
          sha256: slideKey,
          blob,
          mimeType: snapshot.slide?.mimeType ?? blob.type,
          originalName: snapshot.slide?.originalName ?? 'slide',
          sizeBytes: blob.size
        }).then(() => refreshCacheUsage()).catch(() => {});
      }
      nextUrl = URL.createObjectURL(blob);
      if (!cancelled) {
        if (activeImageObjectUrl) URL.revokeObjectURL(activeImageObjectUrl);
        activeImageObjectUrl = nextUrl;
        imageObjectUrl = nextUrl;
        localPage = 1;
      }
    }

    return () => {
      cancelled = true;
      if (nextUrl && nextUrl !== activeImageObjectUrl) URL.revokeObjectURL(nextUrl);
    };
  });

  $effect(() => {
    if (!pdfDocument || !slideCanvas || snapshot.room.roomMode !== 'slides') return;
    stageResizeTick;
    let cancelled = false;
    void renderPDFPage(pdfDocument, slideCanvas, localPage).catch((error) => {
      if (!cancelled) {
        addToast(errorMessage(error, 'Could not render PDF page.'));
      }
    });
    return () => {
      cancelled = true;
    };
  });

  function moveMember(userId: string, direction: -1 | 1) {
    const next = [...snapshot.participants];
    const index = next.findIndex((member) => member.userId === userId);
    if (index < 0) return;
    const target = index + direction;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target], next[index]];
    send({
      type: 'people.reorder',
      payload: {
        orderedUserIds: next.map((member) => member.userId),
        observerUserIds: snapshot.observers.map((member) => member.userId)
      }
    });
  }

  function moveObserver(userId: string, direction: -1 | 1) {
    const next = [...snapshot.observers];
    const index = next.findIndex((member) => member.userId === userId);
    if (index < 0) return;
    const target = index + direction;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target], next[index]];
    send({
      type: 'people.reorder',
      payload: {
        orderedUserIds: snapshot.participants.map((member) => member.userId),
        observerUserIds: next.map((member) => member.userId)
      }
    });
  }

  function setRole(userId: string, role: 'mod' | 'participant' | 'observer') {
    send({ type: 'people.setRole', payload: { userId, role } });
  }

  function setAudioPermission(userId: string, field: 'allowAudioUpload' | 'allowAudioControl', value: boolean) {
    send({ type: 'people.audioPermission', payload: { userId, [field]: value } });
  }

  async function kick(userId: string) {
    const member = [...snapshot.participants, ...snapshot.observers].find((item) => item.userId === userId);
    const confirmed = await confirmAction({
      accent: 'danger',
      title: 'Kick member?',
      message: `Kick ${member?.displayName || userId} from this room?`,
      confirmLabel: 'Kick'
    });
    if (!confirmed) return;
    send({ type: 'people.kick', payload: { userId } });
  }

  function previousTurn() {
    send({ type: 'turn.previous' });
  }

  function nextTurn() {
    send({ type: 'turn.next' });
  }

  function setCurrent(userId: string) {
    send({ type: 'turn.setCurrent', payload: { userId } });
  }

  function startTimer() {
    const durationSeconds = Math.min(Math.max(Math.round(timerDurationSeconds), 1), 86400);
    timerDurationSeconds = durationSeconds;
    send({ type: 'timer.start', payload: { durationSeconds } });
  }

  function toggleTimer() {
    if (snapshot.timer.state === 'running') {
      send({ type: 'timer.stop' });
      return;
    }
    startTimer();
  }

  function resetAndStartTimer() {
    send({ type: 'timer.reset' });
    window.setTimeout(startTimer, 50);
  }

  async function copyCurrentRoomLink() {
    const result = await copyText(window.location.href);
    if (result.copied) {
      addToast('Room link copied', 'success');
      return;
    }
    addToast('Could not copy room link automatically.');
  }

  function playBeep() {
    try {
      const AudioContextConstructor = window.AudioContext || (window as typeof window & { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
      if (!AudioContextConstructor) return;
      const context = new AudioContextConstructor();
      const oscillator = context.createOscillator();
      const gain = context.createGain();
      oscillator.type = 'sine';
      oscillator.frequency.setValueAtTime(880, context.currentTime);
      gain.gain.setValueAtTime(0.1, context.currentTime);
      gain.gain.exponentialRampToValueAtTime(0.001, context.currentTime + 0.5);
      oscillator.connect(gain);
      gain.connect(context.destination);
      oscillator.start();
      oscillator.stop(context.currentTime + 0.5);
    } catch {
      // Timer feedback is best effort; some browsers block programmatic audio.
    }
  }

  function setRaiseHandMode(mode: 'off' | 'manual' | 'queue') {
    send({ type: 'settings.update', payload: { raiseHandMode: mode } });
  }

  function setRoomBooleanSetting(
    name: 'sharedNavigationEnabled' | 'allowParticipantMarkdown' | 'allowAudienceAudioUpload' | 'allowAudienceAudioControl',
    value: boolean
  ) {
    send({ type: 'settings.update', payload: { [name]: value } });
  }

  function setRoomMode(mode: RoomSnapshot['room']['roomMode']) {
    send({ type: 'settings.update', payload: { roomMode: mode } });
  }

  async function saveRoomTitle() {
    settingsMessage = '';
    try {
      await updateRoomSettings(snapshot.room.id, { title: roomTitleDraft });
      settingsMessage = 'Room title saved.';
    } catch (error) {
      addToast(errorMessage(error, 'Room title update failed.'));
    }
  }

  async function setRoomPassword() {
    settingsMessage = '';
    try {
      await updateRoomSettings(snapshot.room.id, { passwordAction: 'set', password: roomPasswordDraft });
      roomPasswordDraft = '';
      settingsMessage = 'Room password updated.';
    } catch (error) {
      addToast(errorMessage(error, 'Password update failed.'));
    }
  }

  async function clearRoomPassword() {
    settingsMessage = '';
    try {
      await updateRoomSettings(snapshot.room.id, { passwordAction: 'clear' });
      roomPasswordDraft = '';
      settingsMessage = 'Room password cleared.';
    } catch (error) {
      addToast(errorMessage(error, 'Password clear failed.'));
    }
  }

  async function copyMigrationLink() {
    settingsMessage = '';
    migrationFallbackText = '';
    try {
      const link = await createMigrationLink(snapshot.room.id);
      const url = new URL(window.location.href);
      url.searchParams.set('room', link.roomId);
      url.searchParams.set('migration', link.migrationId);
      const result = await copyText(url.toString());
      if (result.copied) {
        settingsMessage = `Migration link copied. It expires ${new Date(link.expiresAt).toLocaleString()}.`;
        return;
      }
      migrationFallbackText = result.text;
      settingsMessage = `Select the migration link field and copy it manually. It expires ${new Date(link.expiresAt).toLocaleString()}.`;
      window.setTimeout(() => {
        document.querySelector<HTMLInputElement>('[data-migration-fallback]')?.select();
      });
    } catch (error) {
      addToast(errorMessage(error, 'Migration link creation failed.'));
    }
  }

  function toggleHand() {
    if (callerHand) {
      send({ type: 'hand.lower' });
      return;
    }
    send({ type: 'hand.raise' });
  }

  function raisedHandFor(userId: string) {
    return snapshot.hands.find((hand) => hand.userId === userId);
  }

  function canLowerHandFor(userId: string) {
    return isMod || userId === snapshot.caller.userId;
  }

  function participantStatus(member: SnapshotMember) {
    if (member.userId === snapshot.caller.userId) return 'This is you';
    if (member.userId === snapshot.currentTurn.currentSpeakerUserId) return 'Speaking now';
    if (member.userId === snapshot.currentTurn.nextSpeakerUserId) return 'Up next';
    if (raisedHandFor(member.userId)) return 'Hand raised';
    return 'In speaker list';
  }

  function observerStatus(member: SnapshotMember) {
    if (member.userId === snapshot.caller.userId) return 'You are observing';
    return 'Watching';
  }

  function memberDisplayName(member: SnapshotMember | undefined) {
    if (!member) return 'None';
    return displayNameForRoom(member, allRoomMembers);
  }

  function handDisplayName(userId: string, fallback: string) {
    return memberDisplayName(allRoomMembers.find((member) => member.userId === userId) ?? ({ userId, displayName: fallback, role: 'participant', displayOrder: 0, isOnline: false } as SnapshotMember));
  }

  function callerDisplayName() {
    return memberDisplayName(
      allRoomMembers.find((member) => member.userId === snapshot.caller.userId) ??
        ({ userId: snapshot.caller.userId, displayName: '', role: snapshot.caller.role, displayOrder: 0, isOnline: true } as SnapshotMember)
    );
  }

  function canMoveParticipant(userId: string, direction: -1 | 1) {
    const index = snapshot.participants.findIndex((member) => member.userId === userId);
    return index >= 0 && index + direction >= 0 && index + direction < snapshot.participants.length;
  }

  function canMoveObserver(userId: string, direction: -1 | 1) {
    const index = snapshot.observers.findIndex((member) => member.userId === userId);
    return index >= 0 && index + direction >= 0 && index + direction < snapshot.observers.length;
  }

  function lowerHand(userId: string) {
    send({ type: 'hand.lower', payload: { userId } });
  }

  function navigateSlide(direction: -1 | 1) {
    const maximum = totalPageCount();
    const next = Math.min(Math.max(localPage + direction, 1), maximum);
    localPage = next;
    if (isMod) {
      send({
        type: 'slide.navigate',
        payload: {
          page: next,
          modSharedNavigationEnabled: modShareNavigation
        }
      });
    }
  }

  function submitMarkdown() {
    send({ type: 'markdown.update', payload: { markdown: markdownDraft } });
    markdownMessage = 'Saved.';
  }

  async function submitSlideUpload() {
    if (!slideFile || slideBusy) return;
    slideBusy = true;
    slideProgress = 0;
    slideMessage = 'Hashing slide...';
    try {
      const file = slideFile;
      const sha256 = await sha256File(file);
      const status = snapshot.caller.isAdmin ? await getSlideStatus(sha256) : { alreadyUploaded: false, missing: false };
      if (status.missing) {
        addToast('This slide file was deleted manually on the server.');
        return;
      }
      slideMessage = status.alreadyUploaded ? 'Attaching existing slide...' : 'Uploading slide...';
      await uploadRoomSlide(
        {
          roomId: snapshot.room.id,
          sha256,
          expiresAt: new Date(slideExpiresAt).toISOString(),
          originalName: file.name,
          file: status.alreadyUploaded ? undefined : file
        },
        (percent) => {
          slideProgress = percent;
        }
      );
      void putCachedSlide({
        sha256,
        blob: file,
        mimeType: file.type || slideMimeType,
        originalName: file.name,
        sizeBytes: file.size
      }).then(() => refreshCacheUsage()).catch(() => {});
      slideProgress = 100;
      slideMessage = status.alreadyUploaded ? 'Slide attached.' : 'Slide uploaded.';
      slideFile = null;
    } catch (error) {
      addToast(errorMessage(error, 'Slide upload failed.'));
      slideMessage = '';
    } finally {
      slideBusy = false;
    }
  }

  function setAudioSource(trackId: string, url: string, objectUrl: boolean) {
    if (trackId !== activeAudioTrackId) {
      if (objectUrl) URL.revokeObjectURL(url);
      return;
    }
    const previousTime = audioElement?.currentTime ?? effectiveAudioPositionSeconds;
    const wasPlaying = effectiveAudioState === 'playing' && !audioElement?.paused;
    if (activeAudioObjectUrl && activeAudioObjectUrl !== url) URL.revokeObjectURL(activeAudioObjectUrl);
    activeAudioObjectUrl = objectUrl ? url : '';
    audioObjectUrl = url;
    queueMicrotask(() => {
      if (!audioElement || activeAudioTrackId !== trackId) return;
      if (Number.isFinite(previousTime) && previousTime > 0) {
        try {
          audioElement.currentTime = previousTime;
        } catch {
          // Some streams do not accept seeking until metadata is loaded.
        }
      }
      if (wasPlaying || effectiveAudioState === 'playing') {
        void audioElement.play().catch((error) => {
          if (error.name !== 'AbortError') audioBlocked = true;
        });
      }
    });
  }

  async function downloadAndCacheAudio(track: RoomSnapshot['audio']['tracks'][number], url: string) {
    if (audioDownloadBusy[track.sha256]) return;
    audioDownloadBusy = { ...audioDownloadBusy, [track.sha256]: true };
    audioDownloadProgress = { ...audioDownloadProgress, [track.sha256]: 0 };
    try {
      const response = await fetch(url);
      if (!response.ok) throw new Error('Could not download audio.');
      const total = Number(response.headers.get('Content-Length') ?? track.sizeBytes);
      const reader = response.body?.getReader();
      if (!reader) {
        const blob = await response.blob();
        await cacheDownloadedTrack(track, blob);
        return;
      }
      const chunks: Uint8Array[] = [];
      let loaded = 0;
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        if (!value) continue;
        chunks.push(value);
        loaded += value.byteLength;
        if (total > 0) audioDownloadProgress = { ...audioDownloadProgress, [track.sha256]: Math.min(100, Math.round((loaded / total) * 100)) };
      }
      await cacheDownloadedTrack(track, new Blob(chunks, { type: track.mimeType }));
    } catch (error) {
      if (activeAudioTrackId === track.id) addToast(errorMessage(error, 'Could not cache audio.'));
    } finally {
      audioDownloadBusy = { ...audioDownloadBusy, [track.sha256]: false };
    }
  }

  async function cacheDownloadedTrack(track: RoomSnapshot['audio']['tracks'][number], blob: Blob) {
    await putCachedAudio({
      sha256: track.sha256,
      blob,
      mimeType: track.mimeType,
      originalName: track.originalName,
      uploaderDisplayName: trackUploaderName(track),
      sizeBytes: track.sizeBytes
    });
    audioDownloaded = { ...audioDownloaded, [track.sha256]: true };
    audioDownloadProgress = { ...audioDownloadProgress, [track.sha256]: 100 };
    if (activeAudioTrackId !== track.id) return;
    const cachedURL = URL.createObjectURL(blob);
    setAudioSource(track.id, cachedURL, true);
  }

  async function refreshCacheUsage() {
    const [audio, slides] = await Promise.all([audioCacheStats(), slideCacheStats()]);
    audioCacheUsage = audio;
    slideCacheUsage = slides;
  }

  async function clearCache(kind: 'audio' | 'slides' | 'all') {
    if (cacheBusy) return;
    cacheBusy = true;
    cacheMessage = '';
    try {
      if (kind === 'audio' || kind === 'all') await clearAudioCache();
      if (kind === 'slides' || kind === 'all') await clearSlideCache();
      await refreshCacheUsage();
      cacheMessage = kind === 'all' ? 'Cache cleared.' : `${kind === 'audio' ? 'Audio' : 'Slide'} cache cleared.`;
    } catch (error) {
      addToast(errorMessage(error, 'Could not clear cache.'));
    } finally {
      cacheBusy = false;
    }
  }

  function isInsufficientFreeSpaceError(error: unknown) {
    return errorMessage(error, '') === insufficientFreeSpaceMessage;
  }

  async function checkAudioUploadCapacity(sizeBytes: number) {
    await checkUploadPreflight(sizeBytes);
  }

  async function restoreCachedAudio() {
    if (cacheBusy || audioBusy || !isMod || audioCacheUsage.entries === 0) return;
    cacheBusy = true;
    audioBusy = true;
    audioProgress = 0;
    audioUploadIndex = 0;
    audioWorkTotal = audioCacheUsage.entries;
    cacheMessage = '';
    audioMessage = 'Preparing cached audio...';
    let uploaded = 0;
    let skipped = 0;
    let failed = 0;
    const failureDetails: string[] = [];
    try {
      const entries = await listCachedAudio();
      audioWorkTotal = entries.length;
      if (entries.length === 0) {
        cacheMessage = 'No cached audio to restore.';
        return;
      }
      const roomAudioHashes = new Set(snapshot.audio.tracks.map((track) => track.sha256));
      for (const [index, entry] of entries.entries()) {
        audioUploadIndex = index + 1;
        audioProgress = 0;
        audioMessage = `Restoring ${index + 1} / ${entries.length}...`;
        try {
          if (roomAudioHashes.has(entry.sha256)) {
            skipped += 1;
            audioDownloaded = { ...audioDownloaded, [entry.sha256]: true };
            audioDownloadProgress = { ...audioDownloadProgress, [entry.sha256]: 100 };
            continue;
          }
          await checkAudioUploadCapacity(entry.sizeBytes);
          const file = new File([entry.blob], entry.originalName, { type: entry.mimeType });
          const uploadedTrack = await uploadRoomAudio(
            {
              roomId: snapshot.room.id,
              sha256: entry.sha256,
              originalName: entry.originalName,
              file
            },
            (percent) => {
              audioProgress = percent;
            }
          );
          await updateRoomAudio(snapshot.room.id, uploadedTrack.id, { uploaderDisplayName: entry.uploaderDisplayName?.trim() || hiddenUploaderDisplayName });
          uploaded += 1;
          roomAudioHashes.add(entry.sha256);
          audioDownloaded = { ...audioDownloaded, [entry.sha256]: true };
          audioDownloadProgress = { ...audioDownloadProgress, [entry.sha256]: 100 };
        } catch (error) {
          if (isInsufficientFreeSpaceError(error)) {
            cacheMessage = insufficientFreeSpaceMessage;
            audioMessage = '';
            return;
          }
          failed += 1;
          console.error('Cached audio restore failed', {
            sha256: entry.sha256,
            originalName: entry.originalName,
            mimeType: entry.mimeType,
            sizeBytes: entry.sizeBytes,
            error
          });
          if (failureDetails.length < 5) {
            failureDetails.push(`${entry.originalName}: ${errorMessage(error, 'Upload failed.')}`);
          }
        }
      }
      audioProgress = uploaded > 0 ? 100 : 0;
      audioMessage = uploaded > 0 ? 'Cached audio restored.' : '';
      const moreFailures = failed > failureDetails.length ? ` ${failed - failureDetails.length} more failed.` : '';
      cacheMessage = `${uploaded === 1 ? 'Restored 1 cached audio file.' : `Restored ${uploaded} cached audio files.`} Skipped ${skipped}. Failed ${failed}.${failureDetails.length > 0 ? ` ${failureDetails.join(' ')}` : ''}${moreFailures}`;
      await refreshCacheUsage();
    } catch (error) {
      addToast(errorMessage(error, 'Could not restore cached audio.'));
      audioMessage = '';
    } finally {
      cacheBusy = false;
      audioBusy = false;
    }
  }

  function confirmAction(input: Omit<typeof confirmDialog, 'open' | 'onConfirm'>): Promise<boolean> {
    return new Promise((resolve) => {
      pendingConfirmResolve = resolve;
      confirmDialog = {
        ...input,
        open: true,
        onConfirm: () => {
          confirmDialog = { ...confirmDialog, open: false };
          pendingConfirmResolve = null;
          resolve(true);
        }
      };
    });
  }

  function cancelConfirm() {
    confirmDialog = { ...confirmDialog, open: false };
    pendingConfirmResolve?.(false);
    pendingConfirmResolve = null;
  }

  async function submitAudioUpload() {
    if (audioFiles.length === 0 || audioBusy) return;
    audioBusy = true;
    audioProgress = 0;
    audioUploadIndex = 0;
    audioWorkTotal = audioFiles.length;
    audioMessage = 'Preparing audio...';
    try {
      const { supported, unsupported } = await classifyAudioUploadFiles(audioFiles);
      if (unsupported.length > 0) {
        addToast(`Skipped unsupported audio file${unsupported.length === 1 ? '' : 's'}: ${unsupported.map((file) => file.name).join(', ')}`);
      }
      if (supported.length === 0) {
        audioFiles = [];
        audioWorkTotal = 0;
        audioMessage = 'No supported audio files selected.';
        return;
      }
      audioWorkTotal = supported.length;
      for (const [index, file] of supported.entries()) {
        audioUploadIndex = index + 1;
        try {
          await checkAudioUploadCapacity(file.size);
        } catch (error) {
          if (isInsufficientFreeSpaceError(error)) {
            audioMessage = insufficientFreeSpaceMessage;
            return;
          }
          throw error;
        }
        if (!safeBrowserAudio(file)) {
          const confirmed = await confirmAction({
            accent: 'warning',
            title: 'Upload this audio type?',
            message: `${file.name} may not play in every browser.`,
            confirmLabel: 'Upload anyway'
          });
          if (!confirmed) continue;
        }
        if (snapshot.caller.isAdmin && file.size > 50 * 1024 * 1024) {
          const confirmed = await confirmAction({
            accent: 'warning',
            title: 'Upload as site admin?',
            message: `${file.name} exceeds the participant upload limit.`,
            confirmLabel: 'Upload'
          });
          if (!confirmed) continue;
        }
        audioProgress = 0;
        audioMessage = `Hashing ${index + 1} / ${supported.length}...`;
        const [sha256, metadata] = await Promise.all([sha256File(file), readAudioUploadMetadata(file)]);
        audioMessage = `Uploading ${index + 1} / ${supported.length}...`;
        const uploadedTrack = await uploadRoomAudio(
          {
            roomId: snapshot.room.id,
            sha256,
            originalName: file.name,
            file,
            metadataTitle: metadata.metadataTitle,
            durationSeconds: metadata.durationSeconds,
            cover: metadata.cover
          },
          (percent) => {
            audioProgress = percent;
          }
        );
        void putCachedAudio({
          sha256: uploadedTrack.sha256 || sha256,
          blob: file,
          mimeType: uploadedTrack.mimeType || file.type,
          originalName: uploadedTrack.originalName || file.name,
          uploaderDisplayName: callerDisplayName(),
          sizeBytes: uploadedTrack.sizeBytes || file.size
        }).then(() => refreshCacheUsage()).catch(() => {});
        audioDownloaded = { ...audioDownloaded, [uploadedTrack.sha256 || sha256]: true };
        audioDownloadProgress = { ...audioDownloadProgress, [uploadedTrack.sha256 || sha256]: 100 };
      }
      audioProgress = 100;
      audioMessage = supported.length === 1 ? 'Audio uploaded.' : 'Audio files uploaded.';
      audioFiles = [];
    } catch (error) {
      if (isInsufficientFreeSpaceError(error)) {
        audioMessage = insufficientFreeSpaceMessage;
      } else {
        addToast(errorMessage(error, 'Audio upload failed.'));
        audioMessage = '';
      }
    } finally {
      audioBusy = false;
    }
  }

  async function deleteAudioTrack(trackId: string) {
    const track = snapshot.audio.tracks.find((item) => item.id === trackId);
    const confirmed = await confirmAction({
      accent: 'danger',
      title: 'Remove audio?',
      message: `Remove ${trackDisplayTitle(track)} from this room?`,
      confirmLabel: 'Remove'
    });
    if (!confirmed) return;
    audioMessage = '';
    try {
      await removeRoomAudio(snapshot.room.id, trackId);
      audioMessage = 'Audio removed.';
    } catch (error) {
      addToast(errorMessage(error, 'Audio removal failed.'));
    }
  }

  async function downloadAudio(event: MouseEvent, trackId: string, originalName: string) {
    event.preventDefault();
    const track = snapshot.audio.tracks.find((item) => item.id === trackId);
    if (!track) return;
    const cached = await getCachedAudio(track.sha256);
    const link = document.createElement('a');
    if (cached) {
      const url = URL.createObjectURL(cached.blob);
      link.href = url;
      link.download = originalName;
      link.click();
      URL.revokeObjectURL(url);
      return;
    }
    const tokenLink = await createAudioDownloadLink(snapshot.room.id, trackId);
    link.href = tokenLink.url;
    link.download = originalName;
    link.click();
  }

  function audioTrackMatchesSearch(track: RoomSnapshot['audio']['tracks'][number], query: string) {
    const searchable = [trackDisplayTitle(track), trackUploaderName(track), track.originalName]
      .join(' ')
      .toLowerCase();
    return searchable.includes(query);
  }

  function seedLocalAudioState() {
    localAudioTrackId = effectiveCurrentAudioTrackId || snapshot.audio.currentTrackId || currentAudioTrack?.id || '';
    localAudioPlaybackMode = effectiveAudioPlaybackMode;
    localAudioPositionSeconds = Math.max(0, Math.floor(audioElement?.currentTime ?? effectiveAudioPositionSeconds ?? 0));
    localAudioState = effectiveAudioState;
    localAudioStartedAtMs = localAudioState === 'playing' ? Date.now() - localAudioPositionSeconds * 1000 : 0;
  }

  function usingLocalAudioMode() {
    return manualLocalAudioMode || offlineLocalAudioMode;
  }

  function setManualLocalAudioMode(enabled: boolean) {
    if (enabled && !usingLocalAudioMode()) seedLocalAudioState();
    manualLocalAudioMode = enabled;
  }

  function setAudioTrackElement(element: HTMLElement, trackId: string) {
    audioTrackElements.set(trackId, element);
    return {
      destroy() {
        if (audioTrackElements.get(trackId) === element) audioTrackElements.delete(trackId);
      }
    };
  }

  function showCurrentAudioTrack() {
    const current = audioTrackElements.get(effectiveCurrentAudioTrackId);
    current?.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  }

  function localPreviousAudioTrackId() {
    return nextLocalAudioTrackId(snapshot.room.id, [...snapshot.audio.tracks].reverse(), effectiveCurrentAudioTrackId, effectiveAudioPlaybackMode);
  }

  function localNextAudioTrackId() {
    return nextLocalAudioTrackId(snapshot.room.id, snapshot.audio.tracks, effectiveCurrentAudioTrackId, effectiveAudioPlaybackMode);
  }

  function skipAudio(direction: -1 | 1) {
    const nextTrackId = (direction < 0 ? localPreviousAudioTrackId() : localNextAudioTrackId()) || effectiveCurrentAudioTrackId || currentAudioTrack?.id || '';
    if (!nextTrackId) return;
    playAudio(nextTrackId);
  }

  function seekRelativeAudio(deltaSeconds: number) {
    const current = Math.max(0, Math.floor(audioElement?.currentTime ?? effectiveAudioPositionSeconds ?? 0));
    seekAudio(Math.max(0, current + deltaSeconds));
  }

  function handleMediaPlaybackAction(action: 'play-pause' | 'play' | 'pause' | 'stop' | 'previous' | 'next' | 'seek-backward' | 'seek-forward') {
    if (!canUseAudioControls || !currentAudioTrack) return false;
    if (action === 'play-pause') {
      if (effectiveAudioState === 'playing') pauseAudio();
      else playAudio();
      return true;
    }
    if (action === 'play') {
      playAudio();
      return true;
    }
    if (action === 'pause' || action === 'stop') {
      pauseAudio();
      return true;
    }
    if (action === 'previous') {
      skipAudio(-1);
      return true;
    }
    if (action === 'next') {
      skipAudio(1);
      return true;
    }
    if (action === 'seek-backward') {
      seekRelativeAudio(-10);
      return true;
    }
    seekRelativeAudio(10);
    return true;
  }

  function playAudio(trackId = effectiveCurrentAudioTrackId || currentAudioTrack?.id || '') {
    if (!trackId) return;
    if (usingLocalAudioMode()) {
      const positionSeconds = trackId === effectiveCurrentAudioTrackId ? Math.max(0, Math.floor(audioElement?.currentTime ?? effectiveAudioPositionSeconds)) : 0;
      localAudioTrackId = trackId;
      localAudioPositionSeconds = positionSeconds;
      localAudioStartedAtMs = Date.now() - positionSeconds * 1000;
      localAudioState = 'playing';
      return;
    }
    if (!canControlAudio) return;
    const positionSeconds = trackId === snapshot.audio.currentTrackId ? Math.max(0, Math.floor(audioElement?.currentTime ?? snapshot.audio.positionSeconds)) : 0;
    send({ type: 'audio.play', payload: { trackId, positionSeconds } });
  }

  function pauseAudio() {
    if (usingLocalAudioMode()) {
      localAudioPositionSeconds = Math.max(0, Math.floor(audioElement?.currentTime ?? effectiveAudioPositionSeconds));
      localAudioState = 'paused';
      localAudioStartedAtMs = 0;
      return;
    }
    if (!canControlAudio) return;
    send({ type: 'audio.pause' });
  }

  function toggleLocalAudioMute() {
    audioMuted = !audioMuted;
    saveLocalAudioPreferences(snapshot.room.id, snapshot.caller.userId, { muted: audioMuted, volume: audioVolume });
  }

  function handleLocalAudioMuteClick() {
    if (localVolumeSuppressClick) {
      localVolumeSuppressClick = false;
      return;
    }
    toggleLocalAudioMute();
  }

  function toggleLocalAudioVolume() {
    localVolumeOpen = !localVolumeOpen;
  }

  function startLocalAudioLongPress(event: PointerEvent) {
    if (event.pointerType === 'mouse') return;
    event.preventDefault();
    clearLocalAudioLongPress();
    localVolumePointerId = event.pointerId;
    localVolumePointerTarget = event.currentTarget instanceof HTMLElement ? event.currentTarget : null;
    try {
      localVolumePointerTarget?.setPointerCapture?.(event.pointerId);
    } catch {
      // Some browsers can reject capture if the pointer is already gone.
    }
    localVolumeDragStartY = Number.isFinite(event.clientY) ? event.clientY : 0;
    localVolumeDragStartVolume = audioVolume;
    localVolumeLongPress = window.setTimeout(() => {
      localVolumeOpen = true;
      localVolumeSuppressClick = true;
      localVolumeDragging = true;
      localVolumeLongPress = null;
    }, 450);
  }

  function clearLocalAudioLongPress() {
    if (localVolumeLongPress === null) return;
    window.clearTimeout(localVolumeLongPress);
    localVolumeLongPress = null;
  }

  function releaseLocalVolumePointer() {
    const pointerId = localVolumePointerId;
    if (pointerId !== null) {
      try {
        localVolumePointerTarget?.releasePointerCapture?.(pointerId);
      } catch {
        // Capture may already be released by the browser.
      }
    }
    localVolumePointerId = null;
    localVolumePointerTarget = null;
  }

  function finishLocalAudioPointerPress(event?: PointerEvent) {
    if (event && localVolumePointerId !== null && event.pointerId !== localVolumePointerId) return;
    clearLocalAudioLongPress();
    localVolumeDragging = false;
    releaseLocalVolumePointer();
    if (!localVolumeSuppressClick) return;
    window.setTimeout(() => {
      localVolumeSuppressClick = false;
    }, 0);
  }

  function cancelLocalAudioPointerPress() {
    if (localVolumeDragging) return;
    clearLocalAudioLongPress();
    releaseLocalVolumePointer();
  }

  function handleWindowPointerDown(event: PointerEvent) {
    const target = event.target as Node;
    if (localVolumeOpen && !localVolumeControlElement?.contains(target)) localVolumeOpen = false;
    if (finishModeOpen && !finishModeControlElement?.contains(target)) finishModeOpen = false;
  }

  function handleLocalAudioPointerMove(event: PointerEvent) {
    if (!localVolumeDragging) return;
    if (localVolumePointerId !== null && event.pointerId !== localVolumePointerId) return;
    event.preventDefault();
    if (!Number.isFinite(event.clientY)) return;
    const delta = (localVolumeDragStartY - event.clientY) / 120;
    setLocalAudioVolume(localVolumeDragStartVolume + delta);
  }

  function setLocalAudioVolume(value: number) {
    audioVolume = Math.min(1, Math.max(0, value));
    saveLocalAudioPreferences(snapshot.room.id, snapshot.caller.userId, { muted: audioMuted, volume: audioVolume });
  }

  function seekAudio(value: number) {
    const positionSeconds = Math.max(0, Math.floor(value));
    if (usingLocalAudioMode()) {
      localAudioPositionSeconds = positionSeconds;
      if (audioElement) audioElement.currentTime = positionSeconds;
      if (localAudioState === 'playing') localAudioStartedAtMs = Date.now() - positionSeconds * 1000;
      return;
    }
    if (!canControlAudio) return;
    send({ type: 'audio.seek', payload: { positionSeconds } });
  }

  function selectAudio(trackId: string) {
    if (usingLocalAudioMode()) {
      localAudioTrackId = trackId;
      localAudioPositionSeconds = 0;
      localAudioState = 'paused';
      localAudioStartedAtMs = 0;
      return;
    }
    if (!canControlAudio) return;
    send({ type: 'audio.select', payload: { trackId } });
  }

  function moveAudioTrack(index: number, direction: -1 | 1) {
    if (!isMod) return;
    const next = [...snapshot.audio.tracks];
    const target = index + direction;
    if (target < 0 || target >= next.length) return;
    [next[index], next[target]] = [next[target], next[index]];
    send({ type: 'audio.reorder', payload: { trackIds: next.map((track) => track.id) } });
  }

  function setAudioMode(mode: RoomSnapshot['audio']['playbackMode']) {
    finishModeOpen = false;
    if (usingLocalAudioMode()) {
      localAudioPlaybackMode = mode;
      return;
    }
    if (!canControlAudio) return;
    send({ type: 'audio.mode', payload: { mode } });
  }

  function handleAudioEnded() {
    if (!usingLocalAudioMode()) {
      send({ type: 'audio.ended' }, { notifyOnFailure: false });
      return;
    }
    const nextTrackId = nextLocalAudioTrackId(snapshot.room.id, snapshot.audio.tracks, effectiveCurrentAudioTrackId, effectiveAudioPlaybackMode);
    if (!nextTrackId || effectiveAudioPlaybackMode === 'stop') {
      localAudioState = 'paused';
      localAudioPositionSeconds = 0;
      localAudioStartedAtMs = 0;
      return;
    }
    localAudioTrackId = nextTrackId;
    localAudioPositionSeconds = 0;
    localAudioStartedAtMs = Date.now();
    localAudioState = 'playing';
  }

  function toggleAudioStar(track: RoomSnapshot['audio']['tracks'][number]) {
    if (snapshot.caller.role === 'observer') return;
    send({ type: 'audio.star', payload: { trackId: track.id, starred: !track.starredByCaller } });
  }

  async function saveRoomRetention(neverExpires = false) {
    settingsMessage = '';
    try {
      await updateRoomSettings(snapshot.room.id, neverExpires ? { neverExpires: true } : { expiresAt: new Date(retentionDraft).toISOString() });
      settingsMessage = neverExpires ? 'Room set to never expire.' : 'Room expiration updated.';
    } catch (error) {
      addToast(errorMessage(error, 'Room expiration update failed.'));
    }
  }

  function toggleTrackFromCard(event: Event, track: RoomSnapshot['audio']['tracks'][number]) {
    const target = event.target as Element | null;
    if (target?.closest('button,a,input,form')) return;
    if (!canUseAudioControls) return;
    if (track.id === effectiveCurrentAudioTrackId && effectiveAudioState === 'playing') {
      pauseAudio();
    } else {
      playAudio(track.id);
    }
  }

  function startRenameAudio(track: RoomSnapshot['audio']['tracks'][number]) {
    editingAudioTrackId = track.id;
    audioTitleDraft = trackDisplayTitle(track);
    audioUploaderDraft = trackUploaderName(track);
  }

  async function saveAudioMetadata(track: RoomSnapshot['audio']['tracks'][number]) {
    audioMessage = '';
    try {
      await updateRoomAudio(snapshot.room.id, track.id, {
        title: audioTitleDraft,
        ...(isMod ? { uploaderDisplayName: audioUploaderDraft } : {})
      });
      editingAudioTrackId = '';
      audioMessage = 'Audio details updated.';
    } catch (error) {
      addToast(errorMessage(error, 'Audio update failed.'));
    }
  }

  async function saveSlideExpiration() {
    if (!snapshot.slide) return;
    slideMessage = '';
    try {
      await updateRoomSlideExpiration(snapshot.room.id, new Date(slideExpiresAt).toISOString());
      slideMessage = 'Slide expiration updated.';
    } catch (error) {
      addToast(errorMessage(error, 'Slide expiration update failed.'));
    }
  }

  async function submitRemoveSlide() {
    if (!slideConfirmRemove) {
      slideConfirmRemove = true;
      slideMessage = 'Confirm removing the slide from this room.';
      return;
    }
    slideMessage = '';
    try {
      await removeRoomSlide(snapshot.room.id);
      slideConfirmRemove = false;
      slideFile = null;
      slideMessage = 'Slide removed from room.';
    } catch (error) {
      addToast(errorMessage(error, 'Slide removal failed.'));
    }
  }

  function togglePanel(panel: keyof PanelState) {
    panelState = {
      ...panelState,
      [panel]: !panelState[panel]
    };
  }

  function updateShortcut(action: RebindableShortcutAction, value: string) {
    const result = setShortcutBinding(shortcutConfig, action, value);
    if (result.error) {
      addToast(result.error);
    }
    shortcutConfig = result.config;
    if (!result.error) {
      shortcutDrafts = { ...result.config.bindings };
    }
  }

  function resetShortcut(action: RebindableShortcutAction) {
    shortcutConfig = resetShortcutBinding(shortcutConfig, action);
    shortcutDrafts = { ...shortcutConfig.bindings };
  }

  function setShortcutBoolean(name: 'enabled' | 'modShortcutsEnabled', value: boolean) {
    shortcutConfig = {
      ...shortcutConfig,
      [name]: value
    };
  }

  function mediaKeyAction(key: string): Parameters<typeof handleMediaPlaybackAction>[0] | null {
    if (key === 'MediaPlayPause') return 'play-pause';
    if (key === 'MediaPlay') return 'play';
    if (key === 'MediaPause') return 'pause';
    if (key === 'MediaStop') return 'stop';
    if (key === 'MediaTrackPrevious') return 'previous';
    if (key === 'MediaTrackNext') return 'next';
    if (key === 'MediaRewind') return 'seek-backward';
    if (key === 'MediaFastForward') return 'seek-forward';
    return null;
  }

  function handleKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape' && (localVolumeOpen || finishModeOpen)) {
      localVolumeOpen = false;
      finishModeOpen = false;
      return;
    }
    const mediaAction = mediaKeyAction(event.key);
    if (mediaAction) {
      if (handleMediaPlaybackAction(mediaAction)) event.preventDefault();
      return;
    }
    if (shouldIgnoreShortcut(event)) return;
    const action = resolveShortcutAction(event, shortcutConfig);
    if (!action) return;
    event.preventDefault();
    if (action === 'toggleHelp') {
      panelState = { ...panelState, shortcuts: !panelState.shortcuts };
      return;
    }
    if (!isMod) return;
    if (action === 'previousSpeaker') previousTurn();
    if (action === 'nextSpeaker') nextTurn();
    if (action === 'toggleTimerPause') toggleTimer();
    if (action === 'resetAndStartTimer') resetAndStartTimer();
    if (action === 'previousSlide') navigateSlide(-1);
    if (action === 'nextSlide') navigateSlide(1);
  }

  function formatDuration(seconds: number) {
    const minutes = Math.floor(seconds / 60);
    const remainder = seconds % 60;
    return `${minutes}:${remainder.toString().padStart(2, '0')}`;
  }

  function errorMessage(error: unknown, fallback: string) {
    return error instanceof Error ? error.message : fallback;
  }

  function connectionStatusLabel() {
    if (status === 'connected') return 'Live connection connected';
    if (status === 'connecting') return 'Live connection connecting';
    return 'Live connection disconnected';
  }

  function formatBytes(bytes: number) {
    if (bytes >= 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024 / 1024).toFixed(1)} GB`;
    if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
    if (bytes >= 1024) return `${Math.round(bytes / 1024)} KB`;
    return `${bytes} B`;
  }

  function formatCacheLimit() {
    return `${formatBytes(cacheLimits.maxBytes)} / ${cacheLimits.maxEntries} entries each`;
  }

  function updateAudioTiming() {
    if (!audioElement) return;
    if (!audioSeeking) audioPositionDraft = Math.floor(audioElement.currentTime || 0);
    audioDuration = Math.floor(audioElement.duration || currentAudioTrack?.durationSeconds || effectiveAudioPositionSeconds || 0);
    updateAudioBuffer();
  }

  function updateAudioBuffer() {
    if (!audioElement) return;
    const duration = audioElement.duration || currentAudioTrack?.durationSeconds || 0;
    if (!duration || audioElement.buffered.length === 0) {
      audioBufferedPercent = 0;
      return;
    }
    const end = audioElement.buffered.end(audioElement.buffered.length - 1);
    audioBufferedPercent = Math.min(100, Math.round((end / duration) * 100));
  }

  function defaultExpirationInput() {
    const date = new Date(Date.now() + 14 * 24 * 60 * 60 * 1000);
    return date.toISOString().slice(0, 16);
  }

  async function sha256File(file: File) {
    const digest = await crypto.subtle.digest('SHA-256', await file.arrayBuffer());
    return [...new Uint8Array(digest)].map((byte) => byte.toString(16).padStart(2, '0')).join('');
  }

  function totalPageCount() {
    if (slideIsImage) return 1;
    return Math.max(pdfDocument?.numPages ?? snapshot.room.slidePage ?? 1, 1);
  }

  function loadPanelState(): PanelState {
    const fallback: PanelState = {
      railCollapsed: false,
      participants: true,
      observers: true,
      slides: true,
      audio: true,
      settings: true,
      shortcuts: true,
      cache: true
    };
    try {
      const raw = localStorage.getItem(panelStorageKey);
      if (!raw) return fallback;
      const parsed = JSON.parse(raw) as Partial<PanelState>;
      return {
        railCollapsed: typeof parsed.railCollapsed === 'boolean' ? parsed.railCollapsed : fallback.railCollapsed,
        participants: typeof parsed.participants === 'boolean' ? parsed.participants : fallback.participants,
        observers: typeof parsed.observers === 'boolean' ? parsed.observers : fallback.observers,
        slides: typeof parsed.slides === 'boolean' ? parsed.slides : fallback.slides,
        audio: typeof parsed.audio === 'boolean' ? parsed.audio : fallback.audio,
        settings: typeof parsed.settings === 'boolean' ? parsed.settings : fallback.settings,
        shortcuts: typeof parsed.shortcuts === 'boolean' ? parsed.shortcuts : fallback.shortcuts,
        cache: typeof parsed.cache === 'boolean' ? parsed.cache : fallback.cache
      };
    } catch {
      return fallback;
    }
  }

  function savePanelState(state: PanelState) {
    if (typeof localStorage === 'undefined') return;
    localStorage.setItem(panelStorageKey, JSON.stringify(state));
  }

  async function renderPDFPage(document: PDFDocumentLike, canvas: HTMLCanvasElement, page: number) {
    const pdfPage = await document.getPage(page);
    const containerWidth = canvas.parentElement?.clientWidth ?? 800;
    const containerHeight = canvas.parentElement?.clientHeight ?? 600;
    const baseViewport = pdfPage.getViewport({ scale: 1 });
    const scale = Math.min(containerWidth / baseViewport.width, containerHeight / baseViewport.height, 3);
    const viewport = pdfPage.getViewport({ scale });
    const context = canvas.getContext('2d');
    if (!context) return;
    const ratio = window.devicePixelRatio || 1;
    canvas.width = Math.floor(viewport.width * ratio);
    canvas.height = Math.floor(viewport.height * ratio);
    canvas.style.width = `${Math.floor(viewport.width)}px`;
    canvas.style.height = `${Math.floor(viewport.height)}px`;
    context.setTransform(ratio, 0, 0, ratio, 0, 0);
    await pdfPage.render({ canvasContext: context, viewport }).promise;
  }
</script>

<svelte:window
  onkeydown={handleKeydown}
  onpointerdown={handleWindowPointerDown}
  onpointermove={handleLocalAudioPointerMove}
  onpointerup={finishLocalAudioPointerPress}
  onpointercancel={finishLocalAudioPointerPress}
/>

{#snippet finishModeMenu()}
  {#if canUseAudioControls}
    <div class="finish-mode-control" bind:this={finishModeControlElement}>
      <button
        type="button"
        class="icon-button finish-mode-trigger"
        title={`Finish mode: ${finishModeOptions.find((option) => option.value === effectiveAudioPlaybackMode)?.label ?? effectiveAudioPlaybackMode}`}
        aria-label={`Finish mode: ${finishModeOptions.find((option) => option.value === effectiveAudioPlaybackMode)?.label ?? effectiveAudioPlaybackMode}`}
        aria-haspopup="listbox"
        aria-expanded={finishModeOpen}
        onclick={() => (finishModeOpen = !finishModeOpen)}
      >
        <RotateCcw size={18} />
      </button>
      {#if finishModeOpen}
        <div class="finish-mode-popover" role="listbox" aria-label="Finish mode">
          {#each finishModeOptions as option (option.value)}
            <button
              type="button"
              class:active={option.value === effectiveAudioPlaybackMode}
              role="option"
              aria-selected={option.value === effectiveAudioPlaybackMode}
              onclick={() => setAudioMode(option.value as RoomSnapshot['audio']['playbackMode'])}
            >
              {option.label}
            </button>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
{/snippet}

{#snippet audioMiniPlayer(compact: boolean)}
  <div class={["audio-mini-player", compact && 'compact']}>
    <div class="audio-mini-now">
      {#if !compact}
        <div class="audio-mini-art">
          {#if currentAudioTrack?.hasCover && audioCoverUrls[currentAudioTrack.id]}
            <img src={audioCoverUrls[currentAudioTrack.id]} alt="" />
          {:else}
            <Music size={30} />
          {/if}
        </div>
      {/if}
      <div class="audio-mini-copy">
        <strong>{trackDisplayTitle(currentAudioTrack)}</strong>
        <span>
          {#if currentAudioTrack}
            {trackUploaderName(currentAudioTrack) ? `${trackUploaderName(currentAudioTrack)} · ` : ''}{formatBytes(currentAudioTrack.sizeBytes)} · {audioSubtype(currentAudioTrack.mimeType)}
          {:else}
            Upload a track below to start listening.
          {/if}
        </span>
      </div>
    </div>

    <div class="audio-mini-controls">
      <button class="icon-button audio-transport-button" type="button" disabled={!canUseAudioControls || !currentAudioTrack} onclick={() => skipAudio(-1)} title="Previous track" aria-label="Previous track">
        <SkipBack size={18} />
      </button>
      <button class="icon-button audio-transport-button" type="button" disabled={!canUseAudioControls || !currentAudioTrack} onclick={() => effectiveAudioState === 'playing' ? pauseAudio() : playAudio()} title={effectiveAudioState === 'playing' ? 'Pause' : 'Play'} aria-label={effectiveAudioState === 'playing' ? 'Pause' : 'Play'}>
        {#if effectiveAudioState === 'playing'}
          <Pause size={18} />
        {:else}
          <Play size={18} />
        {/if}
      </button>
      <button class="icon-button audio-transport-button" type="button" disabled={!canUseAudioControls || !currentAudioTrack} onclick={() => skipAudio(1)} title="Next track" aria-label="Next track">
        <SkipForward size={18} />
      </button>
      <button class="icon-button audio-transport-button" type="button" disabled={!currentAudioTrack} onclick={showCurrentAudioTrack} title="Show current track" aria-label="Show current track">
        <Crosshair size={18} />
      </button>
      {@render finishModeMenu()}
      <div class={["local-audio-control", compact && 'compact']} bind:this={localVolumeControlElement}>
        <button
          type="button"
          class="icon-button local-volume-mute"
          class:muted={audioMuted}
          onclick={handleLocalAudioMuteClick}
          onpointerdown={startLocalAudioLongPress}
          onpointermove={handleLocalAudioPointerMove}
          onpointerup={finishLocalAudioPointerPress}
          onpointercancel={cancelLocalAudioPointerPress}
          onpointerleave={cancelLocalAudioPointerPress}
          onlostpointercapture={finishLocalAudioPointerPress}
          title={audioMuted ? "Unmute local audio" : "Mute local audio"}
          aria-label={audioMuted ? "Unmute local audio" : "Mute local audio"}
        >
          {#if audioMuted}
            <VolumeX size={18} />
          {:else}
            <Volume2 size={18} />
          {/if}
        </button>
        <button type="button" class="icon-button local-volume-menu-button" class:active={localVolumeOpen} onclick={toggleLocalAudioVolume} title="Open local audio volume" aria-label="Open local audio volume" aria-expanded={localVolumeOpen}>
          <SlidersHorizontal size={18} />
        </button>
        {#if localVolumeOpen}
          <div class="local-volume-popover" role="group" aria-label="Local audio volume controls" onpointerdown={(event) => event.stopPropagation()}>
            <label>
              <span>Volume {Math.round(audioVolume * 100)}%</span>
              <input
                aria-label="Local audio volume"
                type="range"
                min="0"
                max="100"
                value={Math.round(audioVolume * 100)}
                oninput={(event) => setLocalAudioVolume(Number(event.currentTarget.value) / 100)}
              />
            </label>
          </div>
        {/if}
      </div>
    </div>

    <div class="audio-mini-seek-row">
      <label class="seek-control">
        <input
          type="range"
          min="0"
          max={Math.max(Math.floor(audioDuration || currentAudioTrack?.durationSeconds || effectiveAudioPositionSeconds || 1), 1)}
          value={audioPositionDraft || effectiveAudioPositionSeconds}
          disabled={!canUseAudioControls || !currentAudioTrack}
          onpointerdown={() => (audioSeeking = true)}
          oninput={(event) => (audioPositionDraft = Number(event.currentTarget.value))}
          onchange={(event) => {
            audioSeeking = false;
            seekAudio(Number(event.currentTarget.value));
          }}
        />
        <span class="seek-buffer" style:--buffer={`${audioBufferedPercent}%`}></span>
      </label>
      <span class="audio-time">{formatDuration(audioPositionDraft || effectiveAudioPositionSeconds)} / {formatDuration(audioDuration || currentAudioTrack?.durationSeconds || 0)}</span>
    </div>

    <div class="audio-mini-status-row">
      <label class={["toggle-field", "local-mode-toggle", compact && 'compact']}><input type="checkbox" checked={manualLocalAudioMode} onchange={(event) => setManualLocalAudioMode(event.currentTarget.checked)} /> Local mode (unsynced)</label>
      {#if offlineLocalAudioMode}
        <p class="local-audio-mode-status">Offline local playback</p>
      {/if}
      {#if audioBlocked}
        <button type="button" onclick={() => audioElement?.play()}>Enable audio</button>
      {/if}
    </div>
  </div>
{/snippet}

{#snippet audioListToolbar()}
  <div class="audio-list-toolbar">
    <label class="audio-search-field">
      <span>Search audio</span>
      <Search size={16} aria-hidden="true" />
      <input
        type="text"
        inputmode="search"
        aria-label="Search audio tracks"
        placeholder="Search title, uploader, filename"
        bind:value={audioSearchQuery}
      />
      {#if audioSearchQuery}
        <button class="icon-button audio-search-clear" type="button" title="Clear audio search" aria-label="Clear audio search" onclick={() => (audioSearchQuery = '')}>×</button>
      {/if}
    </label>
    <button class={['icon-text-button', 'starred-filter-button', starredOnly ? 'active-filter' : 'inactive-filter']} type="button" onclick={() => (starredOnly = !starredOnly)}>
      <Star size={16} /> Starred
    </button>
  </div>
{/snippet}

{#snippet audioTrackList(listClass: string)}
  <div class={["audio-track-list", listClass]}>
    {#if displayedAudioTracks.length === 0 && snapshot.audio.tracks.length > 0}
      <p class="audio-search-empty">No audio tracks match this search.</p>
    {/if}
    {#each displayedAudioTracks as track, index (track.id)}
      <div
        class={["audio-track", track.id === effectiveCurrentAudioTrackId && 'current-audio-track']}
        role="button"
        tabindex="0"
        onclick={(event) => toggleTrackFromCard(event, track)}
        onkeydown={(event) => {
          if (event.key === 'Enter' || event.key === ' ') {
            event.preventDefault();
            toggleTrackFromCard(event, track);
          }
        }}
        use:setAudioTrackElement={track.id}
      >
        <div class="audio-track-main">
          <strong>{trackDisplayTitle(track)}</strong>
          <span>{trackUploaderName(track) ? `${trackUploaderName(track)} · ` : ''}{formatBytes(track.sizeBytes)} · {audioSubtype(track.mimeType)}</span>
          {#if audioDownloadBusy[track.sha256] || audioDownloadProgress[track.sha256] > 0}
            <progress max="100" value={audioDownloadProgress[track.sha256] ?? 0}>{audioDownloadProgress[track.sha256] ?? 0}%</progress>
          {/if}
        </div>
        <div class="settings-actions">
          <button class={['icon-button', track.starredByCaller && 'starred-track-button']} type="button" title={track.starredByCaller ? 'Unstar' : 'Star'} aria-label={track.starredByCaller ? 'Unstar' : 'Star'} onclick={(event) => { event.stopPropagation(); toggleAudioStar(track); }}>
            <Star size={16} />
            {#if snapshot.room.showAudioStarCounts && (track.starCount ?? 0) > 0}
              <span>{track.starCount}</span>
            {/if}
          </button>
          {#if isMod}
            <button class="icon-button" type="button" title="Move up" aria-label="Move up" disabled={starredOnly || snapshot.audio.tracks.findIndex((item) => item.id === track.id) === 0} onclick={(event) => { event.stopPropagation(); moveAudioTrack(snapshot.audio.tracks.findIndex((item) => item.id === track.id), -1); }}><ArrowUp size={16} /></button>
            <button class="icon-button" type="button" title="Move down" aria-label="Move down" disabled={starredOnly || snapshot.audio.tracks.findIndex((item) => item.id === track.id) === snapshot.audio.tracks.length - 1} onclick={(event) => { event.stopPropagation(); moveAudioTrack(snapshot.audio.tracks.findIndex((item) => item.id === track.id), 1); }}><ArrowDown size={16} /></button>
          {/if}
          {#if isMod || track.uploadedByUserId === snapshot.caller.userId}
            <button class="icon-button" type="button" title="Rename" aria-label="Rename" onclick={(event) => { event.stopPropagation(); startRenameAudio(track); }}><Pencil size={16} /></button>
          {/if}
          <a class="download-link icon-button" title="Download" aria-label="Download" href={audioFileRequest(snapshot.room.id, track.id).url} onclick={(event) => { event.stopPropagation(); downloadAudio(event, track.id, track.originalName); }}><Download size={16} /></a>
          {#if isMod || track.uploadedByUserId === snapshot.caller.userId}
            <button class="danger-button icon-button" title="Remove" aria-label="Remove" type="button" onclick={(event) => { event.stopPropagation(); deleteAudioTrack(track.id); }}>
              <Trash2 size={16} />
            </button>
          {/if}
        </div>
        {#if editingAudioTrackId === track.id}
          <form class="audio-edit-form" onsubmit={(event) => { event.preventDefault(); saveAudioMetadata(track); }}>
            <input aria-label="Audio title" bind:value={audioTitleDraft} maxlength="200" />
            {#if isMod}
              <input aria-label="Uploader name" bind:value={audioUploaderDraft} maxlength="80" />
            {/if}
            <button type="button" onclick={() => (audioTitleDraft = fileNameWithoutExtension(track.originalName))}>Name</button>
            <button type="button" onclick={() => (audioTitleDraft = track.metadataTitle || fileNameWithoutExtension(track.originalName))}>Title</button>
            <button type="submit">Save</button>
          </form>
        {/if}
      </div>
    {/each}
  </div>
{/snippet}

<section class={['roundtable', panelState.railCollapsed && 'rail-collapsed']} aria-label="Live roundtable">
  {#if canSeeAudio && snapshot.room.roomMode !== 'audio'}
    <audio
      bind:this={audioElement}
      bind:muted={audioMuted}
      src={audioObjectUrl}
      onended={handleAudioEnded}
      ontimeupdate={() => {
        audioPositionDraft = Math.floor(audioElement?.currentTime ?? 0);
      }}
    ></audio>
  {/if}
  <div class="room-stage">
    <div class="top-timer-row" aria-label="Room timer">
      <div class={['top-timer-value', timerEndedPulse && 'timer-ended-pulse', remainingSeconds === 0 && snapshot.timer.state === 'running' && 'timer-ended']}>
        <Timer size={18} />
        <strong>{timerLabel}</strong>
      </div>

      {#if isMod}
        <input class="top-timer-duration" type="number" min="1" max="86400" bind:value={timerDurationSeconds} aria-label="Timer duration" />
        <button class="top-icon-button" type="button" onclick={toggleTimer} title={snapshot.timer.state === 'running' ? 'Pause' : 'Start'} aria-label={snapshot.timer.state === 'running' ? 'Pause timer' : 'Start timer'}>
          {#if snapshot.timer.state === 'running'}
            <Pause size={16} />
          {:else}
            <Play size={16} />
          {/if}
        </button>
        <button class="top-icon-button" type="button" onclick={resetAndStartTimer} title="Reset and start" aria-label="Reset and start timer">
          <RotateCcw size={16} />
        </button>
      {/if}

      <div class="top-timer-speakers" aria-label="Turn summary">
        <span><em>Now:</em> <strong>{memberDisplayName(currentSpeaker)}</strong></span>
        <span><em>Next:</em> <strong>{memberDisplayName(nextSpeaker)}</strong></span>
      </div>

      <div class="top-room-actions">
        <span class={['connection-status-icon', status]} title={connectionStatusLabel()} aria-label={connectionStatusLabel()} role="img">
          <svg viewBox="0 0 24 24" width="18" height="18" aria-hidden="true">
            <circle class="connection-ring" cx="12" cy="12" r="8"></circle>
            {#if status === 'disconnected'}
              <path class="connection-slash" d="M5 19 19 5"></path>
              <circle class="connection-core" cx="12" cy="12" r="3"></circle>
            {:else if status === 'connecting'}
              <path class="connection-arc" d="M12 4a8 8 0 0 1 8 8"></path>
              <circle class="connection-core" cx="12" cy="12" r="3"></circle>
            {:else}
              <circle class="connection-core" cx="12" cy="12" r="3.4"></circle>
              <path class="connection-check" d="m8.6 12.2 2.1 2.1 4.8-5"></path>
            {/if}
          </svg>
        </span>
        <button class="top-icon-button" type="button" onclick={copyCurrentRoomLink} title="Copy room link" aria-label="Copy room link">
          <Copy size={16} />
        </button>
        {#if canUseHands}
          <button class={['top-icon-button', 'top-hand-button', callerHand && 'raised']} type="button" onclick={toggleHand} title={callerHand ? 'Lower hand' : 'Raise hand'} aria-label={callerHand ? 'Lower hand' : 'Raise hand'}>
            <Hand size={16} />
          </button>
        {/if}
      </div>
    </div>

    <section class="document-panel" aria-label={snapshot.room.roomMode === 'audio' ? 'Shared audio' : snapshot.room.roomMode === 'markdown' ? 'Shared markdown' : 'Slides'}>
      {#if snapshot.room.roomMode === 'audio'}
        <div class="audio-stage">
          <audio
            bind:this={audioElement}
            bind:muted={audioMuted}
            src={audioObjectUrl}
            onended={handleAudioEnded}
            ontimeupdate={updateAudioTiming}
            onprogress={updateAudioBuffer}
            onloadedmetadata={updateAudioTiming}
          ></audio>
          <div class="audio-stage-manage">
            {@render audioListToolbar()}
            {@render audioTrackList('audio-stage-track-list')}
            {#if canUploadAudio}
              <form class="audio-upload" onsubmit={(event) => { event.preventDefault(); submitAudioUpload(); }}>
                <label>
                  Audio file
                  <input
                    type="file"
                    accept="audio/*"
                    multiple
                    disabled={audioBusy}
                    onchange={(event) => {
                      audioFiles = Array.from(event.currentTarget.files ?? []);
                      audioMessage = '';
                    }}
                  />
                </label>
                <button type="submit" disabled={audioBusy || audioFiles.length === 0}>
                  <Upload size={16} /> {audioBusy ? `Working ${audioUploadIndex}/${audioWorkTotal}` : 'Upload audio'}
                </button>
                {#if audioBusy || audioProgress > 0}
                  <progress max="100" value={audioProgress}>{audioProgress}%</progress>
                {/if}
              </form>
            {/if}
            {#if audioMessage}
              <p class="upload-message">{audioMessage}</p>
            {/if}
          </div>
          {@render audioMiniPlayer(false)}
        </div>
      {:else if snapshot.room.roomMode === 'markdown'}
        <div class="markdown-panel">
          <div class="document-toolbar">
            <div>
              <p class="kicker">Markdown mode</p>
              <h3>Shared notes</h3>
              {#if snapshot.markdownUpdatedAt}
                <p>Last edited by {snapshot.markdownUpdatedByName || snapshot.markdownUpdatedByUserId} at {new Date(snapshot.markdownUpdatedAt).toLocaleString()}</p>
              {/if}
            </div>
            {#if isMod}
              <label class="toggle-field">
                <input
                  type="checkbox"
                  checked={snapshot.room.allowParticipantMarkdown}
                  onchange={(event) => setRoomBooleanSetting('allowParticipantMarkdown', event.currentTarget.checked)}
                />
                Participant edits
              </label>
            {/if}
          </div>
          <div class="markdown-preview">
            {#if markdownBlocks.length === 0}
              <p>No shared notes yet.</p>
            {:else}
              {#each markdownBlocks as block, index (`${block.kind}-${index}-${block.text}`)}
                {#if block.kind === 'h1'}
                  <h1>{block.text}</h1>
                {:else if block.kind === 'h2'}
                  <h2>{block.text}</h2>
                {:else}
                  <p>{block.text}</p>
                {/if}
              {/each}
            {/if}
          </div>
          {#if markdownEditorVisible}
            <form class="markdown-editor" onsubmit={(event) => { event.preventDefault(); submitMarkdown(); }}>
              <textarea bind:value={markdownDraft} maxlength={65536} rows="8" aria-label="Shared markdown"></textarea>
              <button type="submit">
                <Save size={16} /> Save notes
              </button>
              {#if markdownMessage}
                <p>{markdownMessage}</p>
              {/if}
            </form>
          {/if}
        </div>
      {:else}
        <div class="pdf-panel">
          {#if snapshot.slide && !snapshot.slide.missing}
            <div class="slide-stage-wrap">
              {#if slideIsPDF}
                <canvas bind:this={slideCanvas}></canvas>
              {:else if slideIsImage && imageObjectUrl}
                <img src={imageObjectUrl} alt={snapshot.slide.originalName} />
              {/if}
              <div class="slide-nav-overlay">
                <button type="button" aria-label="Previous slide" onclick={() => navigateSlide(-1)} disabled={localPage <= 1 || !snapshot.slide || snapshot.slide.missing}>
                  <ChevronLeft size={18} />
                </button>
                <span>{localPage} / {totalPageCount()}</span>
                <button type="button" aria-label="Next slide" onclick={() => navigateSlide(1)} disabled={localPage >= totalPageCount() || !snapshot.slide || snapshot.slide.missing}>
                  <ChevronRight size={18} />
                </button>
              </div>
            </div>
          {:else}
            <div class="empty-document">
              <FileText size={34} />
              <p>{snapshot.slide?.missing ? 'The attached slide file is missing.' : 'Upload a slide file to show slides here.'}</p>
              {#if canManageSlides}
                <form class="slide-upload stage-upload" onsubmit={(event) => { event.preventDefault(); submitSlideUpload(); }}>
                  <label>
                    Slide file
                    <input
                      type="file"
                      accept="application/pdf,image/png,image/jpeg,image/webp,image/gif,.pdf,.png,.jpg,.jpeg,.webp,.gif"
                      disabled={slideBusy}
                      onchange={(event) => {
                        slideFile = event.currentTarget.files?.[0] ?? null;
                        slideMessage = '';
                      }}
                    />
                  </label>
                  <label>
                    Expiration
                    <input type="datetime-local" bind:value={slideExpiresAt} disabled={slideBusy} />
                  </label>
                  <button type="submit" disabled={slideBusy || !slideFile}>
                    <Upload size={16} /> {slideBusy ? 'Working' : 'Upload slide'}
                  </button>
                  {#if slideBusy || slideProgress > 0}
                    <progress max="100" value={slideProgress}>{slideProgress}%</progress>
                  {/if}
                  {#if slideMessage}
                    <p class="upload-message">{slideMessage}</p>
                  {/if}
                </form>
              {/if}
            </div>
          {/if}
        </div>
      {/if}
    </section>
  </div>

  {#if panelState.railCollapsed}
    <button class="rail-edge-toggle" type="button" onclick={() => (panelState = { ...panelState, railCollapsed: false })} aria-label="Show room controls">
      <ChevronsLeft size={18} />
    </button>
  {:else}
    <aside class="room-rail">
      <button class="rail-collapse-toggle" type="button" onclick={() => (panelState = { ...panelState, railCollapsed: true })}>
        <ChevronsRight size={18} /> Hide controls
      </button>

      {#if isMod}
        <div class="moderator-controls turn-actions" aria-label="Moderator turn controls">
          <button type="button" title="Previous speaker" onclick={previousTurn}>
            <ChevronLeft size={17} /> Previous
          </button>
          <button type="button" title="Next speaker" onclick={nextTurn}>
            <ChevronRight size={17} /> Next
          </button>
        </div>
      {/if}

      {#if snapshot.hands.length > 0}
      <div class="hand-queue" aria-label="Raised hands">
        <div>
          <Hand size={18} />
          <strong>Raised hands</strong>
        </div>
        {#each snapshot.hands as hand (hand.userId)}
          <span>
            {handDisplayName(hand.userId, hand.displayName)}
            {#if isMod}
              <button type="button" onclick={() => lowerHand(hand.userId)}>Lower</button>
            {/if}
          </span>
        {/each}
      </div>
      {/if}

    <section class="rail-panel">
      <button class="list-toggle" type="button" onclick={() => togglePanel('participants')} aria-expanded={!panelState.participants}>
        <UsersRound size={19} />
        <span>Participants</span>
        <strong>{participantCountLabel}</strong>
      </button>
      {#if !panelState.participants}
        <div class="member-list">
          {#each displayedParticipants as member (member.userId)}
            <article class={['member-row', !member.isOnline && 'offline-row', member.userId === snapshot.currentTurn.currentSpeakerUserId && 'current-speaker-row', member.userId === snapshot.caller.userId && 'self-row']}>
              <div class="member-identity">
                {#if member.userId === snapshot.currentTurn.currentSpeakerUserId}
                  <Mic size={18} />
                {:else if !member.isOnline}
                  <UserX size={18} />
                {:else}
                  <UserRound size={18} />
                {/if}
                <div>
                  <h3>{memberDisplayName(member)}</h3>
                  <p>{participantStatus(member)}</p>
                  {#if member.audioLocalMode}
                    <span class="local-audio-badge" title="Using local audio mode, not synced" aria-label="Using local audio mode, not synced">Local audio</span>
                  {/if}
                </div>
                {#if raisedHandFor(member.userId)}
                  {#if canLowerHandFor(member.userId)}
                    <button class="hand-row-button" type="button" title={`Lower ${memberDisplayName(member)}'s hand`} aria-label={`Lower ${memberDisplayName(member)}'s hand`} onclick={() => lowerHand(member.userId)}>
                      <Hand size={15} />
                    </button>
                  {:else}
                    <span class="hand-row-indicator" title={`${memberDisplayName(member)} has a raised hand`} aria-label={`${memberDisplayName(member)} has a raised hand`}>
                      <Hand size={15} />
                    </span>
                  {/if}
                {/if}
              </div>
              {#if isMod}
                <div class="member-actions">
                  <button class="icon-button" type="button" title="Move up" aria-label="Move up" onclick={() => moveMember(member.userId, -1)} disabled={!canMoveParticipant(member.userId, -1)}><ArrowUp size={16} /></button>
                  <button class="icon-button" type="button" title="Move down" aria-label="Move down" onclick={() => moveMember(member.userId, 1)} disabled={!canMoveParticipant(member.userId, 1)}><ArrowDown size={16} /></button>
                  {#if member.role === 'participant' && !snapshot.room.allowAudienceAudioUpload}
                    <button
                      class={['icon-button', 'permission-button', member.allowAudioUpload && 'active']}
                      type="button"
                      title={member.allowAudioUpload ? 'Revoke audio upload' : 'Grant audio upload'}
                      aria-label={`${member.allowAudioUpload ? 'Revoke' : 'Grant'} ${memberDisplayName(member)} audio upload`}
                      onclick={() => setAudioPermission(member.userId, 'allowAudioUpload', !member.allowAudioUpload)}
                    >
                      <Upload size={16} />
                    </button>
                  {/if}
                  {#if member.role === 'participant' && !snapshot.room.allowAudienceAudioControl}
                    <button
                      class={['icon-button', 'permission-button', member.allowAudioControl && 'active']}
                      type="button"
                      title={member.allowAudioControl ? 'Revoke audio control' : 'Grant audio control'}
                      aria-label={`${member.allowAudioControl ? 'Revoke' : 'Grant'} ${memberDisplayName(member)} audio control`}
                      onclick={() => setAudioPermission(member.userId, 'allowAudioControl', !member.allowAudioControl)}
                    >
                      <Volume2 size={16} />
                    </button>
                  {/if}
                  {#if member.role === 'mod'}
                    <button type="button" onclick={() => setRole(member.userId, 'participant')}>
                      <Shield size={15} /> Demote
                    </button>
                  {:else}
                    <button type="button" onclick={() => setRole(member.userId, 'mod')}>
                      <Shield size={15} /> Promote
                    </button>
                  {/if}
                  <button type="button" onclick={() => setCurrent(member.userId)}>Speak</button>
                  <button type="button" onclick={() => setRole(member.userId, 'observer')}>Observe</button>
                  <button class="danger-button icon-button" type="button" title="Kick" aria-label="Kick" onclick={() => kick(member.userId)}>
                    <LogOut size={15} />
                  </button>
                </div>
              {/if}
            </article>
          {/each}
        </div>
      {/if}
    </section>

    <section class="rail-panel">
      <button class="list-toggle" type="button" onclick={() => togglePanel('observers')} aria-expanded={!panelState.observers}>
        <Eye size={19} />
        <span>Observers</span>
        <strong>{observerCountLabel}</strong>
      </button>
      {#if !panelState.observers}
        <div class="member-list compact">
          {#each displayedObservers as member (member.userId)}
            <article class={['member-row', 'observer-row', !member.isOnline && 'offline-row', member.userId === snapshot.caller.userId && 'self-row']}>
              <div class="member-identity">
                {#if !member.isOnline}
                  <UserX size={18} />
                {:else}
                  <Eye size={18} />
                {/if}
                <div>
                  <h3>{memberDisplayName(member)}</h3>
                  <p>{observerStatus(member)}</p>
                  {#if member.audioLocalMode}
                    <span class="local-audio-badge" title="Using local audio mode, not synced" aria-label="Using local audio mode, not synced">Local audio</span>
                  {/if}
                </div>
              </div>
              {#if isMod || member.userId === snapshot.caller.userId}
                <div class="member-actions">
                  {#if isMod}
                    <button class="icon-button" type="button" title="Move up" aria-label="Move up" onclick={() => moveObserver(member.userId, -1)} disabled={!canMoveObserver(member.userId, -1)}><ArrowUp size={16} /></button>
                    <button class="icon-button" type="button" title="Move down" aria-label="Move down" onclick={() => moveObserver(member.userId, 1)} disabled={!canMoveObserver(member.userId, 1)}><ArrowDown size={16} /></button>
                  {/if}
                  <button type="button" onclick={() => setRole(member.userId, 'participant')}>Rejoin</button>
                  {#if isMod}
                    <button class="danger-button icon-button" type="button" title="Kick" aria-label="Kick" onclick={() => kick(member.userId)}><LogOut size={15} /></button>
                  {/if}
                </div>
              {/if}
            </article>
          {/each}
        </div>
      {/if}
    </section>

    <section class="rail-panel">
      <button class="list-toggle" type="button" onclick={() => togglePanel('slides')} aria-expanded={!panelState.slides}>
        {#if snapshot.slide?.missing}
          <FileWarning size={19} />
        {:else}
          <Upload size={19} />
        {/if}
        <span>Slides</span>
        <strong>{snapshot.slide ? '1' : '0'}</strong>
      </button>
      {#if !panelState.slides}
        <div class="slide-panel" aria-label="Slides">
          {#if snapshot.slide}
            <p class={['slide-state', snapshot.slide.missing && 'missing']}>
              {snapshot.slide.missing ? 'File was deleted manually' : snapshot.slide.originalName}
            </p>
            <p class="slide-expiry">Expires {new Date(snapshot.slide.expiresAt).toLocaleDateString()}</p>
          {:else}
            <p class="slide-state">No slide deck attached</p>
          {/if}
          {#if canManageSlides}
            <form class="slide-upload" onsubmit={(event) => { event.preventDefault(); submitSlideUpload(); }}>
              <label>
                Slide file
                <input
                  type="file"
                  accept="application/pdf,image/png,image/jpeg,image/webp,image/gif,.pdf,.png,.jpg,.jpeg,.webp,.gif"
                  disabled={slideBusy}
                  onchange={(event) => {
                    slideFile = event.currentTarget.files?.[0] ?? null;
                    slideMessage = '';
                  }}
                />
              </label>
              <label>
                Expiration
                <input type="datetime-local" bind:value={slideExpiresAt} disabled={slideBusy} />
              </label>
              <div class="settings-actions">
                <button type="submit" disabled={slideBusy || !slideFile}>
                  <Upload size={16} /> {slideBusy ? 'Working' : 'Replace slide'}
                </button>
                {#if canChangeSlideExpiration}
                  <button type="button" disabled={slideBusy || !snapshot.slide} onclick={saveSlideExpiration}>Save expiration</button>
                {/if}
                <button class="danger-button" type="button" disabled={slideBusy || !snapshot.slide} onclick={submitRemoveSlide}>
                  <Trash2 size={16} /> {slideConfirmRemove ? 'Confirm remove' : 'Remove'}
                </button>
              </div>
              {#if slideBusy || slideProgress > 0}
                <progress max="100" value={slideProgress}>{slideProgress}%</progress>
              {/if}
              {#if slideMessage}
                <p class="upload-message">{slideMessage}</p>
              {/if}
            </form>
          {/if}
          <div class="slide-rail-toggles">
            {#if isMod}
              <label class="toggle-field">
                <input
                  type="checkbox"
                  bind:checked={modShareNavigation}
                  onchange={(event) => setRoomBooleanSetting('sharedNavigationEnabled', event.currentTarget.checked)}
                />
                Share navigation
              </label>
            {/if}
            <label class="toggle-field">
              <input type="checkbox" bind:checked={followSharedNavigation} />
              Follow moderator navigation
            </label>
          </div>
        </div>
      {/if}
    </section>

    {#if canSeeAudio && snapshot.room.roomMode !== 'audio'}
      <section class="rail-panel">
        <button class="list-toggle" type="button" onclick={() => togglePanel('audio')} aria-expanded={!panelState.audio}>
          <Music size={19} />
          <span>Audio</span>
          <strong>{snapshot.audio.tracks.length}</strong>
        </button>
        {#if !panelState.audio}
          <div class="audio-panel" aria-label="Audio">
            <audio
              bind:this={audioElement}
              bind:muted={audioMuted}
              src={audioObjectUrl}
              onended={handleAudioEnded}
              ontimeupdate={updateAudioTiming}
              onprogress={updateAudioBuffer}
              onloadedmetadata={updateAudioTiming}
            ></audio>
            {@render audioListToolbar()}
            {@render audioTrackList('audio-panel-track-list')}
            {#if canUploadAudio}
              <form class="audio-upload" onsubmit={(event) => { event.preventDefault(); submitAudioUpload(); }}>
                <label>
                  Audio file
                  <input
                    type="file"
                    accept="audio/*"
                    multiple
                    disabled={audioBusy}
                    onchange={(event) => {
                      audioFiles = Array.from(event.currentTarget.files ?? []);
                      audioMessage = '';
                    }}
                  />
                </label>
                <button type="submit" disabled={audioBusy || audioFiles.length === 0}>
                  <Upload size={16} /> {audioBusy ? `Working ${audioUploadIndex}/${audioWorkTotal}` : 'Upload audio'}
                </button>
                {#if audioBusy || audioProgress > 0}
                  <progress max="100" value={audioProgress}>{audioProgress}%</progress>
                {/if}
              </form>
            {/if}
            {#if audioMessage}
              <p class="upload-message">{audioMessage}</p>
            {/if}
            {@render audioMiniPlayer(true)}
          </div>
        {/if}
      </section>
    {/if}

    {#if isMod}
      <section class="rail-panel">
        <button class="list-toggle" type="button" onclick={() => togglePanel('settings')} aria-expanded={!panelState.settings}>
          <Settings size={19} />
          <span>Room settings</span>
          <strong>{snapshot.room.raiseHandMode}</strong>
        </button>
        {#if !panelState.settings}
          <div class="settings-panel" aria-label="Room settings">
            <form class="settings-form" onsubmit={(event) => { event.preventDefault(); saveRoomTitle(); }}>
              <label>
                Room title
                <input bind:value={roomTitleDraft} maxlength="120" />
              </label>
              <button type="submit" disabled={roomTitleDraft.trim() === '' || roomTitleDraft.trim() === snapshot.room.title}>
                Save title
              </button>
            </form>
            <form class="settings-form" onsubmit={(event) => { event.preventDefault(); setRoomPassword(); }}>
              <label>
                Room password
                <input bind:value={roomPasswordDraft} type="password" placeholder={snapshot.room.hasPassword ? 'Set a new password' : 'Add a password'} />
              </label>
              <div class="settings-actions">
                <button type="submit" disabled={roomPasswordDraft.trim() === ''}>Set password</button>
                <button class="danger-button" type="button" disabled={!snapshot.room.hasPassword} onclick={clearRoomPassword}>Clear password</button>
              </div>
            </form>
            <SelectMenu
              label="Mode"
              value={snapshot.room.roomMode}
              options={roomModeOptions}
              onChange={(value) => setRoomMode(value as RoomSnapshot['room']['roomMode'])}
            />
            <label class="toggle-field">
              <input
                type="checkbox"
                checked={snapshot.room.allowParticipantMarkdown}
                onchange={(event) => setRoomBooleanSetting('allowParticipantMarkdown', event.currentTarget.checked)}
              />
              Participant markdown edits
            </label>
            <label class="toggle-field">
              <input
                type="checkbox"
                checked={snapshot.room.sharedNavigationEnabled}
                onchange={(event) => setRoomBooleanSetting('sharedNavigationEnabled', event.currentTarget.checked)}
              />
              Shared navigation default
            </label>
            <label class="toggle-field">
              <input
                type="checkbox"
                checked={snapshot.room.allowAudienceAudioUpload}
                onchange={(event) => setRoomBooleanSetting('allowAudienceAudioUpload', event.currentTarget.checked)}
              />
              Audience can upload audio
            </label>
            <label class="toggle-field">
              <input
                type="checkbox"
                checked={snapshot.room.allowAudienceAudioControl}
                onchange={(event) => setRoomBooleanSetting('allowAudienceAudioControl', event.currentTarget.checked)}
              />
              Audience audio controls
            </label>
            <label class="toggle-field">
              <input
                type="checkbox"
                checked={snapshot.room.showAudioStarCounts}
                onchange={(event) => send({ type: 'settings.update', payload: { showAudioStarCounts: event.currentTarget.checked } })}
              />
              Show audio star counts
            </label>
            <SelectMenu
              label="Hands"
              value={snapshot.room.raiseHandMode}
              options={handModeOptions}
              onChange={(value) => setRaiseHandMode(value as 'off' | 'manual' | 'queue')}
            />
            <div class="settings-actions">
              <button type="button" onclick={copyMigrationLink}>
                <Link2 size={16} /> Copy migration link
              </button>
            </div>
            {#if migrationFallbackText}
              <label class="share-fallback">
                Migration link
                <input data-migration-fallback readonly value={migrationFallbackText} />
              </label>
            {/if}
            {#if settingsMessage}
              <p class="upload-message">{settingsMessage}</p>
            {/if}
          </div>
        {/if}
      </section>
    {/if}

    <section class="rail-panel">
      <button class="list-toggle" type="button" onclick={() => { togglePanel('cache'); if (panelState.cache) void refreshCacheUsage(); }} aria-expanded={!panelState.cache}>
        <HardDrive size={19} />
        <span>Cache</span>
        <strong>{formatBytes(audioCacheUsage.bytes + slideCacheUsage.bytes)}</strong>
      </button>
      {#if !panelState.cache}
        <div class="cache-panel" aria-label="Local cache">
          <p class="cache-limit">Local browser cache limit: {formatCacheLimit()}</p>
          <p class="cache-limit">
            Room uploads expire:
            {snapshot.room.neverExpires ? 'never' : snapshot.room.expiresAt ? new Date(snapshot.room.expiresAt).toLocaleString() : 'not set'}
          </p>
          {#if canManageRetention}
            <form class="settings-form" onsubmit={(event) => { event.preventDefault(); saveRoomRetention(false); }}>
              <label>
                Room survival
                <input type="datetime-local" bind:value={retentionDraft} />
              </label>
              <div class="settings-actions">
                <button type="submit" disabled={retentionDraft === ''}>Save survival</button>
                {#if snapshot.caller.isAdmin}
                  <button type="button" onclick={() => saveRoomRetention(true)}>Never expire</button>
                {/if}
              </div>
            </form>
          {/if}
          <div class="cache-usage-grid">
            <div>
              <span>Audio</span>
              <strong>{formatBytes(audioCacheUsage.bytes)}</strong>
              <em>{audioCacheUsage.entries} {audioCacheUsage.entries === 1 ? 'file' : 'files'}</em>
            </div>
            <div>
              <span>Slides</span>
              <strong>{formatBytes(slideCacheUsage.bytes)}</strong>
              <em>{slideCacheUsage.entries} {slideCacheUsage.entries === 1 ? 'file' : 'files'}</em>
            </div>
          </div>
          <div class="settings-actions">
            <button type="button" disabled={cacheBusy} onclick={() => refreshCacheUsage()}>Refresh</button>
            {#if isMod && audioCacheUsage.entries > 0}
              <button type="button" disabled={cacheBusy || audioBusy} onclick={() => restoreCachedAudio()}>Restore cached audio</button>
            {/if}
            <button type="button" disabled={cacheBusy || audioCacheUsage.entries === 0} onclick={() => clearCache('audio')}>Reset audio</button>
            <button type="button" disabled={cacheBusy || slideCacheUsage.entries === 0} onclick={() => clearCache('slides')}>Reset slides</button>
            <button class="danger-button" type="button" disabled={cacheBusy || audioCacheUsage.entries + slideCacheUsage.entries === 0} onclick={() => clearCache('all')}>Reset all</button>
          </div>
          {#if cacheMessage}
            <p class="upload-message">{cacheMessage}</p>
          {/if}
        </div>
      {/if}
    </section>

    <section class="rail-panel">
      <button class="list-toggle" type="button" onclick={() => togglePanel('shortcuts')} aria-expanded={!panelState.shortcuts}>
        <FileText size={19} />
        <span>Shortcuts</span>
        <strong>?</strong>
      </button>
      {#if !panelState.shortcuts}
        <div class="shortcut-panel" aria-label="Keyboard shortcuts">
          <label class="toggle-field">
            <input
              type="checkbox"
              checked={shortcutConfig.enabled}
              onchange={(event) => setShortcutBoolean('enabled', event.currentTarget.checked)}
            />
            Enable shortcuts
          </label>
          <label class="toggle-field">
            <input
              type="checkbox"
              checked={shortcutConfig.modShortcutsEnabled}
              onchange={(event) => setShortcutBoolean('modShortcutsEnabled', event.currentTarget.checked)}
            />
            Enable moderator shortcuts
          </label>
          {#each shortcutActions as action (action)}
            <div class="shortcut-row">
              <label>
                {shortcutLabels[action]}
                <input
                  value={shortcutDrafts[action]}
                  maxlength="16"
                  autocapitalize="off"
                  autocomplete="off"
                  oninput={(event) => (shortcutDrafts = { ...shortcutDrafts, [action]: event.currentTarget.value })}
                  onblur={(event) => updateShortcut(action, event.currentTarget.value)}
                  onkeydown={(event) => {
                    if (event.key === 'Enter') {
                      event.preventDefault();
                      updateShortcut(action, event.currentTarget.value);
                    }
                  }}
                />
              </label>
              <button type="button" onclick={() => resetShortcut(action)}>Reset</button>
            </div>
          {/each}
          <div class="settings-actions">
            <span class="shortcut-fixed"><kbd>?</kbd> opens this panel</span>
          </div>
        </div>
      {/if}
    </section>
    </aside>
  {/if}
</section>

{#if confirmDialog.open}
  <div class={['confirm-backdrop', `confirm-${confirmDialog.accent}`]} role="presentation" onclick={cancelConfirm}>
    <div class="confirm-dialog" role="dialog" aria-modal="true" aria-labelledby="confirm-title" tabindex="-1" onmousedown={(event) => event.stopPropagation()}>
      <h2 id="confirm-title">{confirmDialog.title}</h2>
      <p>{confirmDialog.message}</p>
      <div class="settings-actions">
        <button type="button" onclick={cancelConfirm}>Cancel</button>
        <button class={['danger-button', confirmDialog.accent === 'warning' && 'warning-button']} type="button" onclick={() => confirmDialog.onConfirm()}>
          {confirmDialog.confirmLabel}
        </button>
      </div>
    </div>
  </div>
{/if}

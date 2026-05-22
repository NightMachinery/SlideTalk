<script lang="ts">
  import { onMount } from 'svelte';
  import ArrowDown from '@lucide/svelte/icons/arrow-down';
  import ArrowUp from '@lucide/svelte/icons/arrow-up';
  import ChevronLeft from '@lucide/svelte/icons/chevron-left';
  import ChevronRight from '@lucide/svelte/icons/chevron-right';
  import ChevronsLeft from '@lucide/svelte/icons/chevrons-left';
  import ChevronsRight from '@lucide/svelte/icons/chevrons-right';
  import Eye from '@lucide/svelte/icons/eye';
  import FileText from '@lucide/svelte/icons/file-text';
  import FileWarning from '@lucide/svelte/icons/file-warning';
  import Hand from '@lucide/svelte/icons/hand';
  import Link2 from '@lucide/svelte/icons/link-2';
  import LogOut from '@lucide/svelte/icons/log-out';
  import Download from '@lucide/svelte/icons/download';
  import Pencil from '@lucide/svelte/icons/pencil';
  import Mic from '@lucide/svelte/icons/mic';
  import Music from '@lucide/svelte/icons/music';
  import Pause from '@lucide/svelte/icons/pause';
  import Play from '@lucide/svelte/icons/play';
  import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
  import Save from '@lucide/svelte/icons/save';
  import Settings from '@lucide/svelte/icons/settings';
  import Shield from '@lucide/svelte/icons/shield';
  import Timer from '@lucide/svelte/icons/timer';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import Upload from '@lucide/svelte/icons/upload';
  import UserRound from '@lucide/svelte/icons/user-round';
  import UsersRound from '@lucide/svelte/icons/users-round';
  import { audioCoverRequest, audioFileRequest, createAudioDownloadLink, createMigrationLink, getSlideStatus, removeRoomAudio, removeRoomSlide, slideFileRequest, updateRoomAudio, updateRoomSettings, updateRoomSlideExpiration, uploadRoomAudio, uploadRoomSlide } from '../api';
  import { audioSubtype, fileNameWithoutExtension, gcAudioCache, getCachedAudio, putCachedAudio, trackDisplayTitle, trackUploaderName } from '../audioCache';
  import { readAudioUploadMetadata } from '../audioMetadata';
  import { copyText } from '../clipboard';
  import type { RealtimeCommand, RoomSnapshot, SnapshotMember } from '../realtime';
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
  };

  const panelStorageKey = 'slidetalk.roomPanels.v1';
  const shortcutActions: RebindableShortcutAction[] = ['previousSpeaker', 'nextSpeaker', 'toggleTimer', 'previousSlide', 'nextSlide'];
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
    send
  }: {
    snapshot: RoomSnapshot;
    status: 'connecting' | 'connected' | 'disconnected';
    send: (command: RealtimeCommand) => void;
  } = $props();

  let panelState = $state<PanelState>({
    railCollapsed: false,
    participants: true,
    observers: true,
    slides: true,
    audio: true,
    settings: true,
    shortcuts: true
  });
  let shortcutConfig = $state<ShortcutConfig>(loadShortcutConfig(null));
  let shortcutDrafts = $state<Record<RebindableShortcutAction, string>>({ ...defaultShortcutBindings });
  let shortcutMessage = $state('');
  let preferencesReady = $state(false);
  let timerDurationSeconds = $state(300);
  let nowMs = $state(Date.now());
  let slideFile = $state<File | null>(null);
  let slideExpiresAt = $state(defaultExpirationInput());
  let slideBusy = $state(false);
  let slideProgress = $state(0);
  let slideMessage = $state('');
  let slideError = $state('');
  let slideConfirmRemove = $state(false);
  let slideCanvas = $state<HTMLCanvasElement | null>(null);
  let pdfDocument = $state<PDFDocumentLike | null>(null);
  let pdfError = $state('');
  let imageObjectUrl = $state('');
  let activeImageObjectUrl = '';
  let imageError = $state('');
  let localPage = $state(1);
  let stageResizeTick = $state(0);
  let followSharedNavigation = $state(true);
  let modShareNavigation = $state(false);
  let markdownDraft = $state('');
  let markdownMessage = $state('');
  let roomTitleDraft = $state('');
  let roomPasswordDraft = $state('');
  let settingsMessage = $state('');
  let settingsError = $state('');
  let migrationFallbackText = $state('');
  let audioFiles = $state<File[]>([]);
  let audioElement = $state<HTMLAudioElement | null>(null);
  let audioObjectUrl = $state('');
  let activeAudioObjectUrl = '';
  let activeAudioTrackId = '';
  let audioBusy = $state(false);
  let audioProgress = $state(0);
  let audioUploadIndex = $state(0);
  let audioMessage = $state('');
  let audioError = $state('');
  let audioBlocked = $state(false);
  let audioPositionDraft = $state(0);
  let audioSeeking = $state(false);
  let audioDuration = $state(0);
  let audioBufferedPercent = $state(0);
  let audioDownloadProgress = $state<Record<string, number>>({});
  let audioDownloadBusy = $state<Record<string, boolean>>({});
  let audioDownloaded = $state<Record<string, boolean>>({});
  let audioCoverUrls = $state<Record<string, string>>({});
  let activeAudioCoverObjectUrl = '';
  let editingAudioTrackId = $state('');
  let audioTitleDraft = $state('');
  let audioUploaderDraft = $state('');
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
  const canEditMarkdown = $derived(isMod || (snapshot.caller.role === 'participant' && snapshot.room.allowParticipantMarkdown));
  const currentSpeaker = $derived(snapshot.participants.find((member) => member.userId === snapshot.currentTurn.currentSpeakerUserId));
  const nextSpeaker = $derived(snapshot.participants.find((member) => member.userId === snapshot.currentTurn.nextSpeakerUserId));
  const callerHand = $derived(snapshot.hands.find((hand) => hand.userId === snapshot.caller.userId));
  const canUseHands = $derived(snapshot.caller.role !== 'observer' && snapshot.room.raiseHandMode !== 'off');
  const markdownBlocks = $derived(parseMarkdown(snapshot.markdown || ''));
  const markdownEditorVisible = $derived(snapshot.room.roomMode === 'markdown' && canEditMarkdown);
  const canSeeAudio = $derived(isMod || snapshot.room.allowAudienceAudioAccess);
  const canUploadAudio = $derived(isMod || (snapshot.caller.role === 'participant' && snapshot.room.allowAudienceAudioAccess));
  const canControlAudio = $derived(isMod || (snapshot.caller.role !== 'observer' && snapshot.room.allowAudienceAudioControl));
  const currentAudioTrack = $derived(snapshot.audio.tracks.find((track) => track.id === snapshot.audio.currentTrackId) ?? snapshot.audio.tracks[0]);
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
    void gcAudioCache().catch(() => {});
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
    if (snapshot.slide?.expiresAt) {
      slideExpiresAt = new Date(snapshot.slide.expiresAt).toISOString().slice(0, 16);
    }
  });

  $effect(() => {
    const slideKey = snapshot.slide?.sha256;
    if (!slideKey || snapshot.slide?.missing || snapshot.room.roomMode !== 'slides' || !slideIsPDF) {
      pdfDocument = null;
      pdfError = '';
      return;
    }

    let cancelled = false;
    pdfError = '';
    void loadPDF().catch((error) => {
      if (!cancelled) {
        pdfDocument = null;
        pdfError = error instanceof Error ? error.message : 'Could not load PDF.';
      }
    });

    async function loadPDF() {
      const [{ GlobalWorkerOptions, getDocument }, worker] = await Promise.all([
        import('pdfjs-dist'),
        import('pdfjs-dist/build/pdf.worker.mjs?url')
      ]);
      GlobalWorkerOptions.workerSrc = worker.default;
      const request = slideFileRequest(snapshot.room.id);
      const document = await getDocument({ url: request.url, httpHeaders: request.headers }).promise;
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
    const trackID = snapshot.audio.currentTrackId;
    if (!trackID || !canSeeAudio) {
      if (activeAudioObjectUrl) {
        URL.revokeObjectURL(activeAudioObjectUrl);
        activeAudioObjectUrl = '';
        audioObjectUrl = '';
      }
      activeAudioTrackId = '';
      audioError = '';
      return;
    }

    let cancelled = false;
    let nextBlobUrl = '';
    audioError = '';
    activeAudioTrackId = trackID;
    audioBufferedPercent = 0;
    void loadAudioSource().catch((error) => {
      if (!cancelled) {
        audioError = error instanceof Error ? error.message : 'Could not load audio.';
      }
    });

    async function loadAudioSource() {
      const track = snapshot.audio.tracks.find((item) => item.id === trackID);
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
    if (!audioElement || !audioObjectUrl) return;
    const desired = estimatedAudioSeconds;
    if (Number.isFinite(desired) && Math.abs(audioElement.currentTime - desired) > 2) {
      audioElement.currentTime = desired;
    }
    if (!audioSeeking) audioPositionDraft = Math.floor(audioElement.currentTime || desired);
    if (snapshot.audio.state === 'playing') {
      void audioElement.play().then(() => {
        audioBlocked = false;
      }).catch(() => {
        audioBlocked = true;
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
      imageError = '';
      return;
    }

    let cancelled = false;
    let nextUrl = '';
    imageError = '';
    void loadImage().catch((error) => {
      if (!cancelled) {
        imageError = error instanceof Error ? error.message : 'Could not load image slide.';
      }
    });

    async function loadImage() {
      const request = slideFileRequest(snapshot.room.id);
      const response = await fetch(request.url, { headers: request.headers });
      if (!response.ok) throw new Error('Could not load image slide.');
      const blob = await response.blob();
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
        pdfError = error instanceof Error ? error.message : 'Could not render PDF page.';
      }
    });
    return () => {
      cancelled = true;
    };
  });

  function moveMember(list: SnapshotMember[], index: number, direction: -1 | 1) {
    const next = [...list];
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

  function moveObserver(index: number, direction: -1 | 1) {
    const next = [...snapshot.observers];
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

  function resetTimer() {
    send({ type: 'timer.reset' });
  }

  function setRaiseHandMode(mode: 'off' | 'manual' | 'queue') {
    send({ type: 'settings.update', payload: { raiseHandMode: mode } });
  }

  function setRoomBooleanSetting(
    name: 'sharedNavigationEnabled' | 'allowParticipantMarkdown' | 'allowAudienceAudioAccess' | 'allowAudienceAudioControl',
    value: boolean
  ) {
    send({ type: 'settings.update', payload: { [name]: value } });
  }

  function setRoomMode(mode: RoomSnapshot['room']['roomMode']) {
    send({ type: 'settings.update', payload: { roomMode: mode } });
  }

  async function saveRoomTitle() {
    settingsError = '';
    settingsMessage = '';
    try {
      await updateRoomSettings(snapshot.room.id, { title: roomTitleDraft });
      settingsMessage = 'Room title saved.';
    } catch (error) {
      settingsError = error instanceof Error ? error.message : 'Room title update failed.';
    }
  }

  async function setRoomPassword() {
    settingsError = '';
    settingsMessage = '';
    try {
      await updateRoomSettings(snapshot.room.id, { passwordAction: 'set', password: roomPasswordDraft });
      roomPasswordDraft = '';
      settingsMessage = 'Room password updated.';
    } catch (error) {
      settingsError = error instanceof Error ? error.message : 'Password update failed.';
    }
  }

  async function clearRoomPassword() {
    settingsError = '';
    settingsMessage = '';
    try {
      await updateRoomSettings(snapshot.room.id, { passwordAction: 'clear' });
      roomPasswordDraft = '';
      settingsMessage = 'Room password cleared.';
    } catch (error) {
      settingsError = error instanceof Error ? error.message : 'Password clear failed.';
    }
  }

  async function copyMigrationLink() {
    settingsError = '';
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
      settingsError = error instanceof Error ? error.message : 'Migration link creation failed.';
    }
  }

  function toggleHand() {
    if (callerHand) {
      send({ type: 'hand.lower' });
      return;
    }
    send({ type: 'hand.raise' });
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
    slideError = '';
    try {
      const sha256 = await sha256File(slideFile);
      const status = snapshot.caller.isAdmin ? await getSlideStatus(sha256) : { alreadyUploaded: false, missing: false };
      if (status.missing) {
        slideError = 'This slide file was deleted manually on the server.';
        return;
      }
      slideMessage = status.alreadyUploaded ? 'Attaching existing slide...' : 'Uploading slide...';
      await uploadRoomSlide(
        {
          roomId: snapshot.room.id,
          sha256,
          expiresAt: new Date(slideExpiresAt).toISOString(),
          originalName: slideFile.name,
          file: status.alreadyUploaded ? undefined : slideFile
        },
        (percent) => {
          slideProgress = percent;
        }
      );
      slideProgress = 100;
      slideMessage = status.alreadyUploaded ? 'Slide attached.' : 'Slide uploaded.';
      slideFile = null;
    } catch (error) {
      slideError = error instanceof Error ? error.message : 'Slide upload failed.';
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
    const previousTime = audioElement?.currentTime ?? estimatedAudioSeconds;
    const wasPlaying = snapshot.audio.state === 'playing' && !audioElement?.paused;
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
      if (wasPlaying || snapshot.audio.state === 'playing') {
        void audioElement.play().catch(() => {
          audioBlocked = true;
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
      if (activeAudioTrackId === track.id) audioError = error instanceof Error ? error.message : 'Could not cache audio.';
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
      sizeBytes: track.sizeBytes
    });
    audioDownloaded = { ...audioDownloaded, [track.sha256]: true };
    audioDownloadProgress = { ...audioDownloadProgress, [track.sha256]: 100 };
    if (activeAudioTrackId !== track.id) return;
    const cachedURL = URL.createObjectURL(blob);
    setAudioSource(track.id, cachedURL, true);
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
    audioMessage = 'Preparing audio...';
    audioError = '';
    try {
      for (const [index, file] of audioFiles.entries()) {
        audioUploadIndex = index + 1;
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
        audioMessage = `Hashing ${index + 1} / ${audioFiles.length}...`;
        const [sha256, metadata] = await Promise.all([sha256File(file), readAudioUploadMetadata(file)]);
        audioMessage = `Uploading ${index + 1} / ${audioFiles.length}...`;
        await uploadRoomAudio(
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
      }
      audioProgress = 100;
      audioMessage = audioFiles.length === 1 ? 'Audio uploaded.' : 'Audio files uploaded.';
      audioFiles = [];
    } catch (error) {
      audioError = error instanceof Error ? error.message : 'Audio upload failed.';
      audioMessage = '';
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
    audioError = '';
    audioMessage = '';
    try {
      await removeRoomAudio(snapshot.room.id, trackId);
      audioMessage = 'Audio removed.';
    } catch (error) {
      audioError = error instanceof Error ? error.message : 'Audio removal failed.';
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

  function playAudio(trackId = snapshot.audio.currentTrackId || currentAudioTrack?.id || '') {
    if (!trackId || !canControlAudio) return;
    const positionSeconds = trackId === snapshot.audio.currentTrackId ? Math.max(0, Math.floor(audioElement?.currentTime ?? snapshot.audio.positionSeconds)) : 0;
    send({ type: 'audio.play', payload: { trackId, positionSeconds } });
  }

  function pauseAudio() {
    if (!canControlAudio) return;
    send({ type: 'audio.pause' });
  }

  function seekAudio(value: number) {
    if (!canControlAudio) return;
    const positionSeconds = Math.max(0, Math.floor(value));
    send({ type: 'audio.seek', payload: { positionSeconds } });
  }

  function selectAudio(trackId: string) {
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
    if (!isMod) return;
    send({ type: 'audio.mode', payload: { mode } });
  }

  function toggleTrackFromCard(event: Event, track: RoomSnapshot['audio']['tracks'][number]) {
    const target = event.target as Element | null;
    if (target?.closest('button,a,input,form')) return;
    if (!canControlAudio) return;
    if (track.id === snapshot.audio.currentTrackId && snapshot.audio.state === 'playing') {
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
    audioError = '';
    audioMessage = '';
    try {
      await updateRoomAudio(snapshot.room.id, track.id, {
        title: audioTitleDraft,
        ...(isMod ? { uploaderDisplayName: audioUploaderDraft } : {})
      });
      editingAudioTrackId = '';
      audioMessage = 'Audio details updated.';
    } catch (error) {
      audioError = error instanceof Error ? error.message : 'Audio update failed.';
    }
  }

  async function saveSlideExpiration() {
    if (!snapshot.slide) return;
    slideError = '';
    slideMessage = '';
    try {
      await updateRoomSlideExpiration(snapshot.room.id, new Date(slideExpiresAt).toISOString());
      slideMessage = 'Slide expiration updated.';
    } catch (error) {
      slideError = error instanceof Error ? error.message : 'Slide expiration update failed.';
    }
  }

  async function submitRemoveSlide() {
    if (!slideConfirmRemove) {
      slideConfirmRemove = true;
      slideMessage = 'Confirm removing the slide from this room.';
      slideError = '';
      return;
    }
    slideError = '';
    slideMessage = '';
    try {
      await removeRoomSlide(snapshot.room.id);
      slideConfirmRemove = false;
      slideFile = null;
      slideMessage = 'Slide removed from room.';
    } catch (error) {
      slideError = error instanceof Error ? error.message : 'Slide removal failed.';
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
    shortcutMessage = result.error;
    shortcutConfig = result.config;
    if (!result.error) {
      shortcutDrafts = { ...result.config.bindings };
    }
  }

  function resetShortcut(action: RebindableShortcutAction) {
    shortcutMessage = '';
    shortcutConfig = resetShortcutBinding(shortcutConfig, action);
    shortcutDrafts = { ...shortcutConfig.bindings };
  }

  function setShortcutBoolean(name: 'enabled' | 'modShortcutsEnabled', value: boolean) {
    shortcutMessage = '';
    shortcutConfig = {
      ...shortcutConfig,
      [name]: value
    };
  }

  function handleKeydown(event: KeyboardEvent) {
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
    if (action === 'toggleTimer') toggleTimer();
    if (action === 'previousSlide') navigateSlide(-1);
    if (action === 'nextSlide') navigateSlide(1);
  }

  function formatDuration(seconds: number) {
    const minutes = Math.floor(seconds / 60);
    const remainder = seconds % 60;
    return `${minutes}:${remainder.toString().padStart(2, '0')}`;
  }

  function formatBytes(bytes: number) {
    if (bytes >= 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
    if (bytes >= 1024) return `${Math.round(bytes / 1024)} KB`;
    return `${bytes} B`;
  }

  function updateAudioTiming() {
    if (!audioElement) return;
    if (!audioSeeking) audioPositionDraft = Math.floor(audioElement.currentTime || 0);
    audioDuration = Math.floor(audioElement.duration || currentAudioTrack?.durationSeconds || estimatedAudioSeconds || 0);
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

  function safeBrowserAudio(file: File) {
    const type = file.type.toLowerCase();
    return ['audio/mpeg', 'audio/mp4', 'audio/aac', 'audio/ogg', 'audio/opus', 'audio/wav', 'audio/flac', 'audio/webm', 'audio/x-m4a'].includes(type);
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
        shortcuts: true
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
        shortcuts: typeof parsed.shortcuts === 'boolean' ? parsed.shortcuts : fallback.shortcuts
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

<svelte:window onkeydown={handleKeydown} />

<section class={['roundtable', panelState.railCollapsed && 'rail-collapsed']} aria-label="Live roundtable">
  {#if canSeeAudio && snapshot.room.roomMode !== 'audio'}
    <audio
      bind:this={audioElement}
      src={audioObjectUrl}
      onended={() => send({ type: 'audio.ended' })}
      ontimeupdate={() => {
        audioPositionDraft = Math.floor(audioElement?.currentTime ?? 0);
      }}
    ></audio>
  {/if}
  <div class="room-stage">
    <div class="timer-row" aria-label="Room timer">
      <div class="timer-row-value">
        <Timer size={18} />
        <strong>{timerLabel}</strong>
      </div>
      {#if isMod}
        <div class="timer-row-controls" aria-label="Moderator timer controls">
          <label class="compact-field">
            Timer
            <input type="number" min="1" max="86400" bind:value={timerDurationSeconds} />
          </label>
          <button type="button" onclick={toggleTimer}>{snapshot.timer.state === 'running' ? 'Stop' : 'Start'}</button>
          <button type="button" title="Reset timer" onclick={resetTimer}>
            <RotateCcw size={16} /> Reset
          </button>
        </div>
      {:else if canUseHands}
        <button class="hand-toggle compact-hand" type="button" onclick={toggleHand}>
          <Hand size={17} /> {callerHand ? 'Lower hand' : 'Raise hand'}
        </button>
      {/if}
    </div>

    <section class="document-panel" aria-label={snapshot.room.roomMode === 'audio' ? 'Shared audio' : snapshot.room.roomMode === 'markdown' ? 'Shared markdown' : 'Slides'}>
      {#if snapshot.room.roomMode === 'audio'}
        <div class="audio-stage">
          <audio
            bind:this={audioElement}
            src={audioObjectUrl}
            onended={() => send({ type: 'audio.ended' })}
            ontimeupdate={updateAudioTiming}
            onprogress={updateAudioBuffer}
            onloadedmetadata={updateAudioTiming}
          ></audio>
          <div class="audio-stage-art">
            {#if currentAudioTrack?.hasCover && audioCoverUrls[currentAudioTrack.id]}
              <img src={audioCoverUrls[currentAudioTrack.id]} alt="" />
            {:else}
              <Music size={48} />
            {/if}
          </div>
          <div class="audio-stage-copy">
            <p class="kicker">Audio mode</p>
            <h3>{trackDisplayTitle(currentAudioTrack)}</h3>
            <p>
              {#if currentAudioTrack}
                {trackUploaderName(currentAudioTrack) ? `${trackUploaderName(currentAudioTrack)} · ` : ''}{formatBytes(currentAudioTrack.sizeBytes)} · {audioSubtype(currentAudioTrack.mimeType)}
              {:else}
                Upload a track below to start listening.
              {/if}
            </p>
          </div>
          <div class="audio-stage-controls">
            <button type="button" disabled={!canControlAudio || !currentAudioTrack} onclick={() => snapshot.audio.state === 'playing' ? pauseAudio() : playAudio()}>
              {#if snapshot.audio.state === 'playing'}
                <Pause size={18} /> Pause
              {:else}
                <Play size={18} /> Play
              {/if}
            </button>
            <label class="seek-control">
              <input
                type="range"
                min="0"
                max={Math.max(Math.floor(audioDuration || currentAudioTrack?.durationSeconds || estimatedAudioSeconds || 1), 1)}
                value={audioPositionDraft || estimatedAudioSeconds}
                disabled={!canControlAudio || !currentAudioTrack}
                onpointerdown={() => (audioSeeking = true)}
                oninput={(event) => (audioPositionDraft = Number(event.currentTarget.value))}
                onchange={(event) => {
                  audioSeeking = false;
                  seekAudio(Number(event.currentTarget.value));
                }}
              />
              <span class="seek-buffer" style:--buffer={`${audioBufferedPercent}%`}></span>
            </label>
            <span class="audio-time">{formatDuration(audioPositionDraft || estimatedAudioSeconds)} / {formatDuration(audioDuration || currentAudioTrack?.durationSeconds || 0)}</span>
          </div>
          {#if audioBlocked}
            <button type="button" onclick={() => audioElement?.play()}>Enable audio</button>
          {/if}
          <div class="audio-stage-manage">
            {#if isMod}
              <SelectMenu
                label="Finish"
                value={snapshot.audio.playbackMode}
                options={finishModeOptions}
                onChange={(value) => setAudioMode(value as RoomSnapshot['audio']['playbackMode'])}
              />
            {/if}
            <div class="audio-track-list">
              {#each snapshot.audio.tracks as track, index (track.id)}
                <div
                  class={["audio-track", track.id === snapshot.audio.currentTrackId && 'current-audio-track']}
                  role="button"
                  tabindex="0"
                  onclick={(event) => toggleTrackFromCard(event, track)}
                  onkeydown={(event) => {
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault();
                      toggleTrackFromCard(event, track);
                    }
                  }}
                >
                  <div class="audio-track-main">
                    <strong>{trackDisplayTitle(track)}</strong>
                    <span>{trackUploaderName(track) ? `${trackUploaderName(track)} · ` : ''}{formatBytes(track.sizeBytes)} · {audioSubtype(track.mimeType)}</span>
                    {#if audioDownloadBusy[track.sha256] || audioDownloadProgress[track.sha256] > 0}
                      <progress max="100" value={audioDownloadProgress[track.sha256] ?? 0}>{audioDownloadProgress[track.sha256] ?? 0}%</progress>
                    {/if}
                  </div>
                  <div class="settings-actions">
                    {#if isMod}
                      <button class="icon-button" type="button" title="Move up" aria-label="Move up" disabled={index === 0} onclick={(event) => { event.stopPropagation(); moveAudioTrack(index, -1); }}><ArrowUp size={16} /></button>
                      <button class="icon-button" type="button" title="Move down" aria-label="Move down" disabled={index === snapshot.audio.tracks.length - 1} onclick={(event) => { event.stopPropagation(); moveAudioTrack(index, 1); }}><ArrowDown size={16} /></button>
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
                      audioError = '';
                    }}
                  />
                </label>
                <button type="submit" disabled={audioBusy || audioFiles.length === 0}>
                  <Upload size={16} /> {audioBusy ? `Working ${audioUploadIndex}/${audioFiles.length}` : 'Upload audio'}
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
          {#if audioError}
            <p class="upload-error" role="alert">{audioError}</p>
          {/if}
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
                        slideError = '';
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
          {#if pdfError}
            <p class="upload-error" role="alert">{pdfError}</p>
          {/if}
          {#if imageError}
            <p class="upload-error" role="alert">{imageError}</p>
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
            {hand.displayName}
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
        <strong>{snapshot.participants.length}</strong>
      </button>
      {#if !panelState.participants}
        <div class="member-list">
          {#each snapshot.participants as member, index (member.userId)}
            <article class={['member-row', member.userId === snapshot.currentTurn.currentSpeakerUserId && 'current-speaker-row']}>
              <div class="member-identity">
                {#if member.userId === snapshot.currentTurn.currentSpeakerUserId}
                  <Mic size={18} />
                {:else}
                  <UserRound size={18} />
                {/if}
                <div>
                  <h3>{member.displayName}</h3>
                  <p>{member.role}</p>
                </div>
              </div>
              {#if isMod}
                <div class="member-actions">
                  <button class="icon-button" type="button" title="Move up" aria-label="Move up" onclick={() => moveMember(snapshot.participants, index, -1)} disabled={index === 0}><ArrowUp size={16} /></button>
                  <button class="icon-button" type="button" title="Move down" aria-label="Move down" onclick={() => moveMember(snapshot.participants, index, 1)} disabled={index === snapshot.participants.length - 1}><ArrowDown size={16} /></button>
                  {#if member.role !== 'mod'}
                    <button type="button" onclick={() => setRole(member.userId, 'mod')}>
                      <Shield size={15} /> Mod
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
        <strong>{snapshot.observers.length}</strong>
      </button>
      {#if !panelState.observers}
        <div class="member-list compact">
          {#each snapshot.observers as member, index (member.userId)}
            <article class="member-row observer-row">
              <div class="member-identity">
                <Eye size={18} />
                <div>
                  <h3>{member.displayName}</h3>
                  <p>observer</p>
                </div>
              </div>
              {#if isMod}
                <div class="member-actions">
                  <button class="icon-button" type="button" title="Move up" aria-label="Move up" onclick={() => moveObserver(index, -1)} disabled={index === 0}><ArrowUp size={16} /></button>
                  <button class="icon-button" type="button" title="Move down" aria-label="Move down" onclick={() => moveObserver(index, 1)} disabled={index === snapshot.observers.length - 1}><ArrowDown size={16} /></button>
                  <button type="button" onclick={() => setRole(member.userId, 'participant')}>Talk</button>
                  <button class="danger-button icon-button" type="button" title="Kick" aria-label="Kick" onclick={() => kick(member.userId)}><LogOut size={15} /></button>
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
                    slideError = '';
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
              {#if slideError}
                <p class="upload-error" role="alert">{slideError}</p>
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
              src={audioObjectUrl}
              onended={() => send({ type: 'audio.ended' })}
              ontimeupdate={updateAudioTiming}
              onprogress={updateAudioBuffer}
              onloadedmetadata={updateAudioTiming}
            ></audio>
            <div class="audio-now">
              <strong>{trackDisplayTitle(currentAudioTrack)}</strong>
              <span>{currentAudioTrack ? `${trackUploaderName(currentAudioTrack) ? `${trackUploaderName(currentAudioTrack)} · ` : ''}${formatBytes(currentAudioTrack.sizeBytes)} · ${audioSubtype(currentAudioTrack.mimeType)}` : `${snapshot.audio.state} · ${snapshot.audio.playbackMode}`}</span>
              <div class="settings-actions">
                <button type="button" disabled={!canControlAudio || !currentAudioTrack} onclick={() => snapshot.audio.state === 'playing' ? pauseAudio() : playAudio()}>
                  {#if snapshot.audio.state === 'playing'}
                    <Pause size={16} /> Pause
                  {:else}
                    <Play size={16} /> Play
                  {/if}
                </button>
                {#if audioBlocked}
                  <button type="button" onclick={() => audioElement?.play()}>Enable audio</button>
                {/if}
              </div>
            </div>
            {#if isMod}
              <SelectMenu
                label="Finish"
                value={snapshot.audio.playbackMode}
                options={finishModeOptions}
                onChange={(value) => setAudioMode(value as RoomSnapshot['audio']['playbackMode'])}
              />
            {/if}
            <div class="audio-track-list">
              {#each snapshot.audio.tracks as track, index (track.id)}
                <div
                  class={["audio-track", track.id === snapshot.audio.currentTrackId && 'current-audio-track']}
                  role="button"
                  tabindex="0"
                  onclick={(event) => toggleTrackFromCard(event, track)}
                  onkeydown={(event) => {
                    if (event.key === 'Enter' || event.key === ' ') {
                      event.preventDefault();
                      toggleTrackFromCard(event, track);
                    }
                  }}
                >
                  <div class="audio-track-main">
                    <strong>{trackDisplayTitle(track)}</strong>
                    <span>{trackUploaderName(track) ? `${trackUploaderName(track)} · ` : ''}{formatBytes(track.sizeBytes)} · {audioSubtype(track.mimeType)}</span>
                    {#if audioDownloadBusy[track.sha256] || audioDownloadProgress[track.sha256] > 0}
                      <progress max="100" value={audioDownloadProgress[track.sha256] ?? 0}>{audioDownloadProgress[track.sha256] ?? 0}%</progress>
                    {/if}
                  </div>
                  <div class="settings-actions">
                    {#if isMod}
                      <button class="icon-button" type="button" title="Move up" aria-label="Move up" disabled={index === 0} onclick={(event) => { event.stopPropagation(); moveAudioTrack(index, -1); }}><ArrowUp size={16} /></button>
                      <button class="icon-button" type="button" title="Move down" aria-label="Move down" disabled={index === snapshot.audio.tracks.length - 1} onclick={(event) => { event.stopPropagation(); moveAudioTrack(index, 1); }}><ArrowDown size={16} /></button>
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
                      audioError = '';
                    }}
                  />
                </label>
                <button type="submit" disabled={audioBusy || audioFiles.length === 0}>
                  <Upload size={16} /> {audioBusy ? `Working ${audioUploadIndex}/${audioFiles.length}` : 'Upload audio'}
                </button>
                {#if audioBusy || audioProgress > 0}
                  <progress max="100" value={audioProgress}>{audioProgress}%</progress>
                {/if}
              </form>
            {/if}
            {#if audioMessage}
              <p class="upload-message">{audioMessage}</p>
            {/if}
            {#if audioError}
              <p class="upload-error" role="alert">{audioError}</p>
            {/if}
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
                checked={snapshot.room.allowAudienceAudioAccess}
                onchange={(event) => setRoomBooleanSetting('allowAudienceAudioAccess', event.currentTarget.checked)}
              />
              Audience audio access
            </label>
            <label class="toggle-field">
              <input
                type="checkbox"
                checked={snapshot.room.allowAudienceAudioControl}
                onchange={(event) => setRoomBooleanSetting('allowAudienceAudioControl', event.currentTarget.checked)}
              />
              Audience audio controls
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
            {#if settingsError}
              <p class="upload-error" role="alert">{settingsError}</p>
            {/if}
          </div>
        {/if}
      </section>
    {/if}

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
          {#if shortcutMessage}
            <p class="upload-error" role="alert">{shortcutMessage}</p>
          {/if}
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

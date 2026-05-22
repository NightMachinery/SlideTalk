<script lang="ts">
  import { onMount } from 'svelte';
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
  import Mic from '@lucide/svelte/icons/mic';
  import RotateCcw from '@lucide/svelte/icons/rotate-ccw';
  import Save from '@lucide/svelte/icons/save';
  import Settings from '@lucide/svelte/icons/settings';
  import Shield from '@lucide/svelte/icons/shield';
  import Timer from '@lucide/svelte/icons/timer';
  import Trash2 from '@lucide/svelte/icons/trash-2';
  import Upload from '@lucide/svelte/icons/upload';
  import UserRound from '@lucide/svelte/icons/user-round';
  import UsersRound from '@lucide/svelte/icons/users-round';
  import { createMigrationLink, getSlideStatus, removeRoomSlide, slideFileRequest, updateRoomSettings, updateRoomSlideExpiration, uploadRoomSlide } from '../api';
  import { copyText } from '../clipboard';
  import type { RealtimeCommand, RoomSnapshot, SnapshotMember } from '../realtime';
  import { parseMarkdown } from './markdown';
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
    settings: boolean;
    shortcuts: boolean;
  };

  const panelStorageKey = 'slidetalk.roomPanels.v1';
  const shortcutActions: RebindableShortcutAction[] = ['previousSpeaker', 'nextSpeaker', 'toggleTimer', 'previousSlide', 'nextSlide'];

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

  const isMod = $derived(snapshot.caller.role === 'mod');
  const canManageSlides = $derived(isMod);
  const canChangeSlideExpiration = $derived(isMod && snapshot.caller.isAdmin);
  const canEditMarkdown = $derived(isMod || (snapshot.caller.role === 'participant' && snapshot.room.allowParticipantMarkdown));
  const currentSpeaker = $derived(snapshot.participants.find((member) => member.userId === snapshot.currentTurn.currentSpeakerUserId));
  const nextSpeaker = $derived(snapshot.participants.find((member) => member.userId === snapshot.currentTurn.nextSpeakerUserId));
  const callerHand = $derived(snapshot.hands.find((hand) => hand.userId === snapshot.caller.userId));
  const canUseHands = $derived(snapshot.caller.role !== 'observer' && snapshot.room.raiseHandMode !== 'off');
  const markdownBlocks = $derived(parseMarkdown(snapshot.markdown || ''));
  const markdownEditorVisible = $derived(snapshot.room.noSlideMode && canEditMarkdown);
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

  onMount(() => {
    panelState = loadPanelState();
    shortcutConfig = loadShortcutConfig();
    shortcutDrafts = { ...shortcutConfig.bindings };
    preferencesReady = true;
    const interval = window.setInterval(() => {
      nowMs = Date.now();
    }, 1000);
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
    if (!slideKey || snapshot.slide?.missing || snapshot.room.noSlideMode || !slideIsPDF) {
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
    const slideKey = snapshot.slide?.sha256;
    if (!slideKey || snapshot.slide?.missing || snapshot.room.noSlideMode || !slideIsImage) {
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
    if (!pdfDocument || !slideCanvas || snapshot.room.noSlideMode) return;
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

  function kick(userId: string) {
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

  function setRoomBooleanSetting(name: 'sharedNavigationEnabled' | 'noSlideMode' | 'allowParticipantMarkdown', value: boolean) {
    send({ type: 'settings.update', payload: { [name]: value } });
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

    <section class="document-panel" aria-label={snapshot.room.noSlideMode ? 'Shared markdown' : 'Slides'}>
      {#if snapshot.room.noSlideMode}
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
                  <button type="button" onclick={() => moveMember(snapshot.participants, index, -1)} disabled={index === 0}>Up</button>
                  <button type="button" onclick={() => moveMember(snapshot.participants, index, 1)} disabled={index === snapshot.participants.length - 1}>Down</button>
                  {#if member.role !== 'mod'}
                    <button type="button" onclick={() => setRole(member.userId, 'mod')}>
                      <Shield size={15} /> Mod
                    </button>
                  {/if}
                  <button type="button" onclick={() => setCurrent(member.userId)}>Speak</button>
                  <button type="button" onclick={() => setRole(member.userId, 'observer')}>Observe</button>
                  <button class="danger-button" type="button" onclick={() => kick(member.userId)}>
                    <LogOut size={15} /> Kick
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
                  <button type="button" onclick={() => moveObserver(index, -1)} disabled={index === 0}>Up</button>
                  <button type="button" onclick={() => moveObserver(index, 1)} disabled={index === snapshot.observers.length - 1}>Down</button>
                  <button type="button" onclick={() => setRole(member.userId, 'participant')}>Talk</button>
                  <button class="danger-button" type="button" onclick={() => kick(member.userId)}>Kick</button>
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
            <label class="toggle-field">
              <input
                type="checkbox"
                checked={snapshot.room.noSlideMode}
                onchange={(event) => setRoomBooleanSetting('noSlideMode', event.currentTarget.checked)}
              />
              No-slide markdown mode
            </label>
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
            <label class="compact-field">
              Hands
              <select value={snapshot.room.raiseHandMode} onchange={(event) => setRaiseHandMode(event.currentTarget.value as 'off' | 'manual' | 'queue')}>
                <option value="off">Off</option>
                <option value="manual">Manual</option>
                <option value="queue">Queue</option>
              </select>
            </label>
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

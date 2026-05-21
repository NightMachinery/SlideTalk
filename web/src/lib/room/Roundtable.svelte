<script lang="ts">
  import { onMount } from 'svelte';
  import { ChevronLeft, ChevronRight, Eye, FileText, FileWarning, Hand, Link2, LogOut, Mic, RotateCcw, Save, Settings, Shield, Timer, Trash2, Upload, UserRound, UsersRound } from '@lucide/svelte';
  import { createMigrationLink, getSlideStatus, removeRoomSlide, slideFileRequest, updateRoomSettings, updateRoomSlideExpiration, uploadRoomSlide } from '../api';
  import { copyText } from '../clipboard';
  import type { RealtimeCommand, RoomSnapshot, SnapshotMember } from '../realtime';
  import { parseMarkdown } from './markdown';
  import { pageFromSharedNavigation } from './slides';
  import { shouldIgnoreShortcut } from './shortcuts';

  type PDFDocumentLike = {
    numPages: number;
    getPage(page: number): Promise<{
      getViewport(input: { scale: number }): { width: number; height: number };
      render(input: { canvasContext: CanvasRenderingContext2D; viewport: { width: number; height: number } }): {
        promise: Promise<void>;
      };
    }>;
  };

  let {
    snapshot,
    status,
    send
  }: {
    snapshot: RoomSnapshot;
    status: 'connecting' | 'connected' | 'disconnected';
    send: (command: RealtimeCommand) => void;
  } = $props();

  let participantsCollapsed = $state(false);
  let observersCollapsed = $state(false);
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
  let localPage = $state(1);
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
    const interval = window.setInterval(() => {
      nowMs = Date.now();
    }, 1000);
    return () => window.clearInterval(interval);
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
    if (!slideKey || snapshot.slide?.missing || snapshot.room.noSlideMode) {
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
    if (!pdfDocument || !slideCanvas || snapshot.room.noSlideMode) return;
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

  function handleKeydown(event: KeyboardEvent) {
    if (shouldIgnoreShortcut(event)) return;
    if (!isMod) return;
    if (event.key === 'b') {
      event.preventDefault();
      previousTurn();
    }
    if (event.key === 'n') {
      event.preventDefault();
      nextTurn();
    }
    if (event.key === 't') {
      event.preventDefault();
      toggleTimer();
    }
    if (event.key === '[') {
      if (!modShareNavigation) return;
      event.preventDefault();
      navigateSlide(-1);
    }
    if (event.key === ']') {
      if (!modShareNavigation) return;
      event.preventDefault();
      navigateSlide(1);
    }
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
    return Math.max(pdfDocument?.numPages ?? snapshot.room.slidePage ?? 1, 1);
  }

  async function renderPDFPage(document: PDFDocumentLike, canvas: HTMLCanvasElement, page: number) {
    const pdfPage = await document.getPage(page);
    const containerWidth = canvas.parentElement?.clientWidth ?? 800;
    const baseViewport = pdfPage.getViewport({ scale: 1 });
    const scale = Math.min(containerWidth / baseViewport.width, 1.8);
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

<section class="roundtable" aria-label="Live roundtable">
  <div class="room-board">
    <div class="room-board-header">
      <div>
        <p class="kicker">Live room</p>
        <h2>{snapshot.room.title}</h2>
        <p class="next-speaker">Up next: {nextSpeaker?.displayName ?? 'No one queued'}</p>
      </div>
      <span class={['connection-pill', status]}>{status}</span>
    </div>

    <div class="workspace-console" class:no-slide-mode={snapshot.room.noSlideMode}>
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
            <div class="document-toolbar">
              <div>
                <p class="kicker">Slide deck</p>
                <h3>{snapshot.slide?.originalName ?? 'No PDF attached'}</h3>
                {#if snapshot.slide?.missing}
                  <p class="missing">File was deleted manually.</p>
                {:else if snapshot.slide}
                  <p>Page {localPage} of {totalPageCount()}</p>
                {/if}
              </div>
              <div class="slide-controls">
                <button type="button" onclick={() => navigateSlide(-1)} disabled={localPage <= 1 || !snapshot.slide || snapshot.slide.missing}>
                  <ChevronLeft size={16} /> Page
                </button>
                <button type="button" onclick={() => navigateSlide(1)} disabled={localPage >= totalPageCount() || !snapshot.slide || snapshot.slide.missing}>
                  Page <ChevronRight size={16} />
                </button>
              </div>
            </div>
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
            {#if snapshot.slide && !snapshot.slide.missing}
              <div class="pdf-canvas-wrap">
                <canvas bind:this={slideCanvas}></canvas>
              </div>
            {:else}
              <div class="empty-document">
                <FileText size={34} />
                <p>{snapshot.slide?.missing ? 'The attached slide file is missing.' : 'Upload a PDF to show slides here.'}</p>
              </div>
            {/if}
            {#if pdfError}
              <p class="upload-error" role="alert">{pdfError}</p>
            {/if}
          </div>
        {/if}
      </section>

      <aside class="room-rail">
        {#if isMod}
          <section class="settings-panel" aria-label="Room settings">
            <div class="panel-title">
              <Settings size={18} />
              <h3>Room settings</h3>
            </div>
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
          </section>
        {/if}

        <div class="turn-console" aria-label="Turn controls">
      <div class="current-speaker-card">
        <Mic size={24} />
        <div>
          <span>Current speaker</span>
          <strong>{currentSpeaker?.displayName ?? 'No active speaker'}</strong>
        </div>
      </div>
      <div class="timer-card">
        <Timer size={22} />
        <div>
          <span>{snapshot.timer.state === 'running' ? 'Timer running' : 'Timer stopped'}</span>
          <strong>{timerLabel}</strong>
        </div>
      </div>
      {#if isMod}
        <div class="moderator-controls" aria-label="Moderator turn controls">
          <button type="button" title="Previous speaker" onclick={previousTurn}>
            <ChevronLeft size={17} /> Previous
          </button>
          <button type="button" title="Next speaker" onclick={nextTurn}>
            <ChevronRight size={17} /> Next
          </button>
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
        <button class="hand-toggle" type="button" onclick={toggleHand}>
          <Hand size={17} /> {callerHand ? 'Lower hand' : 'Raise hand'}
        </button>
      {/if}
        </div>

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

    <div class="speaker-ledger">
      <button class="list-toggle" type="button" onclick={() => (participantsCollapsed = !participantsCollapsed)}>
        <UsersRound size={19} />
        <span>Participants</span>
        <strong>{snapshot.participants.length}</strong>
      </button>
      {#if !participantsCollapsed}
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
    </div>
      </aside>
    </div>
  </div>

  <aside class="observer-panel">
    <div class="slide-panel" aria-label="Slides">
      <div class="panel-title">
        {#if snapshot.slide?.missing}
          <FileWarning size={19} />
        {:else}
          <Upload size={19} />
        {/if}
        <h2>Slides</h2>
      </div>
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
            PDF
            <input
              type="file"
              accept="application/pdf,.pdf"
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
              <Upload size={16} /> {slideBusy ? 'Working' : 'Replace PDF'}
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
    </div>

    <button class="list-toggle" type="button" onclick={() => (observersCollapsed = !observersCollapsed)}>
      <Eye size={19} />
      <span>Observers</span>
      <strong>{snapshot.observers.length}</strong>
    </button>
    {#if !observersCollapsed}
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
  </aside>
</section>

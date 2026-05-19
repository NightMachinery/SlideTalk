<script lang="ts">
  import { onMount } from 'svelte';
  import { ChevronLeft, ChevronRight, Eye, Hand, LogOut, Mic, RotateCcw, Shield, Timer, UserRound, UsersRound } from '@lucide/svelte';
  import type { RealtimeCommand, RoomSnapshot, SnapshotMember } from '../realtime';
  import { shouldIgnoreShortcut } from './shortcuts';

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

  const isMod = $derived(snapshot.caller.role === 'mod');
  const currentSpeaker = $derived(snapshot.participants.find((member) => member.userId === snapshot.currentTurn.currentSpeakerUserId));
  const nextSpeaker = $derived(snapshot.participants.find((member) => member.userId === snapshot.currentTurn.nextSpeakerUserId));
  const callerHand = $derived(snapshot.hands.find((hand) => hand.userId === snapshot.caller.userId));
  const canUseHands = $derived(snapshot.caller.role !== 'observer' && snapshot.room.raiseHandMode !== 'off');
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
  }

  function formatDuration(seconds: number) {
    const minutes = Math.floor(seconds / 60);
    const remainder = seconds % 60;
    return `${minutes}:${remainder.toString().padStart(2, '0')}`;
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
          <label class="compact-field">
            Hands
            <select value={snapshot.room.raiseHandMode} onchange={(event) => setRaiseHandMode(event.currentTarget.value as 'off' | 'manual' | 'queue')}>
              <option value="off">Off</option>
              <option value="manual">Manual</option>
              <option value="queue">Queue</option>
            </select>
          </label>
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
  </div>

  <aside class="observer-panel">
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

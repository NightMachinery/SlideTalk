<script lang="ts">
  import { onMount } from 'svelte';
  import { CirclePlus, DoorOpen, KeyRound, PanelsTopLeft, ShieldCheck, UserRound } from '@lucide/svelte';
  import {
    createRoom,
    getMe,
    joinRoom,
    submitAdminToken,
    updateProfile,
    type RoomDetails,
    type User
  } from './lib/api';
  import { connectRealtime, type RealtimeConnection, type RealtimeEvent, type RoomSnapshot } from './lib/realtime';
  import Roundtable from './lib/room/Roundtable.svelte';

  let user = $state<User | null>(null);
  let room = $state<RoomDetails | null>(null);
  let snapshot = $state<RoomSnapshot | null>(null);
  let realtime = $state<RealtimeConnection | null>(null);
  let connectionStatus = $state<'connecting' | 'connected' | 'disconnected'>('disconnected');
  let displayName = $state('');
  let adminToken = $state('');
  let createTitle = $state('');
  let createPassword = $state('');
  let joinRoomId = $state('');
  let joinPassword = $state('');
  let loading = $state(true);
  let busy = $state(false);
  let errorMessage = $state('');
  let notice = $state('');

  const needsProfile = $derived(Boolean(user && user.displayName.trim() === ''));

  onMount(async () => {
    await loadProfile();
  });

  async function loadProfile() {
    loading = true;
    errorMessage = '';
    try {
      user = await getMe();
      displayName = user.displayName;
    } catch (error) {
      errorMessage = messageFrom(error);
    } finally {
      loading = false;
    }
  }

  async function saveProfile() {
    await run(async () => {
      user = await updateProfile(displayName);
      displayName = user.displayName;
      notice = 'Profile saved.';
    });
  }

  async function promoteAdmin() {
    await run(async () => {
      user = await submitAdminToken(adminToken);
      adminToken = '';
      notice = 'Admin access enabled.';
    });
  }

  async function submitCreateRoom() {
    await run(async () => {
      room = await createRoom(createTitle, createPassword);
      await openRealtime(room.room.id);
      createTitle = '';
      createPassword = '';
      notice = `Created ${room.room.title}.`;
    });
  }

  async function submitJoinRoom() {
    await run(async () => {
      room = await joinRoom(joinRoomId, joinPassword);
      await openRealtime(room.room.id);
      joinPassword = '';
      notice = `Joined ${room.room.title}.`;
    });
  }

  async function run(task: () => Promise<void>) {
    busy = true;
    errorMessage = '';
    notice = '';
    try {
      await task();
    } catch (error) {
      errorMessage = messageFrom(error);
    } finally {
      busy = false;
    }
  }

  function messageFrom(error: unknown) {
    return error instanceof Error ? error.message : 'Something went wrong.';
  }

  async function openRealtime(roomId: string) {
    realtime?.close();
    snapshot = null;
    connectionStatus = 'connecting';
    realtime = await connectRealtime(
      roomId,
      (event: RealtimeEvent) => {
        if (event.type === 'room.snapshot' && event.payload) {
          snapshot = event.payload;
        }
        if (event.type === 'error') {
          errorMessage = event.message ?? 'Realtime command failed.';
        }
      },
      (status) => {
        connectionStatus = status;
      }
    );
  }
</script>

<svelte:head>
  <meta
    name="description"
    content="SlideTalk coordinates roundtable speaking order, slides, and timers for online discussions."
  />
</svelte:head>

<main class="app-shell">
  <nav class="topbar" aria-label="Primary navigation">
    <a class="brand" href="/" aria-label="SlideTalk home">
      <PanelsTopLeft size={22} strokeWidth={2.2} />
      <span>SlideTalk</span>
    </a>
    <div class="topbar-actions" aria-label="Profile status">
      <span class:admin={user?.isAdmin} class="status-dot" aria-hidden="true"></span>
      <span>{user?.isAdmin ? 'Site admin' : user?.displayName || 'Local identity'}</span>
    </div>
  </nav>

  {#if loading}
    <section class="loading-panel" aria-live="polite">Loading local identity...</section>
  {:else}
    <section class="workspace" aria-labelledby="workspace-title">
      <div class="intro">
        <p class="kicker">Room control</p>
        <h1 id="workspace-title">Set your name, then open a roundtable room.</h1>
        <p class="summary">
          Your browser keeps a private local token. The server remembers the display name for
          that token, so refreshes and room rejoins do not ask again.
        </p>
      </div>

      <aside class="profile-panel" aria-label="Profile settings">
        <div class="panel-title">
          <UserRound size={20} />
          <h2>Profile</h2>
        </div>
        <label>
          Display name
          <input bind:value={displayName} maxlength="80" autocomplete="name" placeholder="Ada" />
        </label>
        <button type="button" disabled={busy || displayName.trim() === ''} onclick={saveProfile}>
          Save profile
        </button>

        <div class="admin-box">
          <div class="panel-title">
            <ShieldCheck size={20} />
            <h2>Admin token</h2>
          </div>
          <label>
            Bootstrap token
            <input bind:value={adminToken} autocomplete="off" placeholder="Paste token" />
          </label>
          <button type="button" disabled={busy || adminToken.trim() === ''} onclick={promoteAdmin}>
            Enable admin
          </button>
        </div>
      </aside>
    </section>

    <section class="control-grid" aria-label="Room actions">
      <form class="command-panel primary-panel" onsubmit={(event) => { event.preventDefault(); submitCreateRoom(); }}>
        <div class="panel-title">
          <CirclePlus size={22} />
          <h2>Create room</h2>
        </div>
        <label>
          Room title
          <input bind:value={createTitle} placeholder="Tuesday facilitation circle" disabled={needsProfile} />
        </label>
        <label>
          Optional password
          <input bind:value={createPassword} type="password" placeholder="Leave empty for open rooms" disabled={needsProfile} />
        </label>
        <button type="submit" disabled={busy || needsProfile || createTitle.trim() === ''}>
          Create room
        </button>
      </form>

      <form class="command-panel" onsubmit={(event) => { event.preventDefault(); submitJoinRoom(); }}>
        <div class="panel-title">
          <DoorOpen size={22} />
          <h2>Join room</h2>
        </div>
        <label>
          Room ID
          <input bind:value={joinRoomId} placeholder="Room link or ID" disabled={needsProfile} />
        </label>
        <label>
          Password
          <input bind:value={joinPassword} type="password" placeholder="Only if required" disabled={needsProfile} />
        </label>
        <button type="submit" disabled={busy || needsProfile || joinRoomId.trim() === ''}>
          Join room
        </button>
      </form>
    </section>

    {#if errorMessage}
      <p class="feedback error" role="alert">{errorMessage}</p>
    {/if}
    {#if notice}
      <p class="feedback notice" role="status">{notice}</p>
    {/if}
    {#if needsProfile}
      <p class="feedback notice" role="status">Save a display name before creating or joining rooms.</p>
    {/if}

    {#if snapshot && realtime}
      <Roundtable
        {snapshot}
        status={connectionStatus}
        send={(command) => {
          realtime?.send(command);
        }}
      />
    {:else if room}
      <section class="room-summary" aria-label="Joined room">
        <div>
          <div class="panel-title">
            <KeyRound size={20} />
            <h2>{room.room.title}</h2>
          </div>
          <p>Role: {room.membership.role}. Room ID: <code>{room.room.id}</code></p>
        </div>
        <span>{room.room.hasPassword ? 'Password protected' : 'Open room'}</span>
      </section>
    {/if}
  {/if}
</main>

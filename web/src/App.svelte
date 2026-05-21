<script lang="ts">
  import { onMount } from 'svelte';
  import { CirclePlus, DoorOpen, KeyRound, PanelsTopLeft, ShieldCheck, UserRound } from '@lucide/svelte';
  import {
    createRoom,
    demoteAdmin,
    demoteAllAdmins,
    getRoom,
    getMe,
    getRoomSnapshot,
    joinRoom,
    listAdmins,
    submitAdminToken,
    updateProfile,
    type Admin,
    type RoomDetails,
    type User
  } from './lib/api';
  import { copyText } from './lib/clipboard';
  import { connectRealtime, type RealtimeConnection, type RealtimeEvent, type RoomSnapshot } from './lib/realtime';
  import { roomIdFromInput } from './lib/roomLink';
  import Roundtable from './lib/room/Roundtable.svelte';

  let user = $state<User | null>(null);
  let room = $state<RoomDetails | null>(null);
  let snapshot = $state<RoomSnapshot | null>(null);
  let realtime = $state<RealtimeConnection | null>(null);
  let connectionStatus = $state<'connecting' | 'connected' | 'disconnected'>('disconnected');
  let displayName = $state('');
  let adminToken = $state('');
  let admins = $state<Admin[]>([]);
  let adminPanelMessage = $state('');
  let confirmDemoteUserId = $state('');
  let confirmDemoteAll = $state(false);
  let createTitle = $state('');
  let createPassword = $state('');
  let joinRoomId = $state('');
  let joinPassword = $state('');
  let joinMigrationId = $state('');
  let loading = $state(true);
  let busy = $state(false);
  let errorMessage = $state('');
  let notice = $state('');
  let shareFallbackText = $state('');

  const needsProfile = $derived(Boolean(user && user.displayName.trim() === ''));

  onMount(async () => {
    await loadProfile();
    const roomParam = new URL(window.location.href).searchParams.get('room');
    if (roomParam) {
      joinRoomId = roomParam;
    }
    const migrationParam = new URL(window.location.href).searchParams.get('migration');
    if (migrationParam) {
      joinMigrationId = migrationParam;
    }
    if (roomParam && user && user.displayName.trim() !== '') {
      await autoOpenRoom(roomParam, migrationParam ?? '');
    }
  });

  async function loadProfile() {
    loading = true;
    errorMessage = '';
    try {
      user = await getMe();
      displayName = user.displayName;
      if (user.isAdmin) {
        admins = await listAdmins();
      }
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
      admins = await listAdmins();
      notice = 'Admin access enabled.';
    });
  }

  async function refreshAdmins() {
    if (!user?.isAdmin) return;
    admins = await listAdmins();
    adminPanelMessage = 'Admin list updated.';
  }

  async function submitDemoteAdmin(admin: Admin) {
    if (confirmDemoteUserId !== admin.id) {
      confirmDemoteUserId = admin.id;
      adminPanelMessage = `Confirm demoting ${admin.displayName || admin.id}.`;
      return;
    }
    await run(async () => {
      await demoteAdmin(admin.id);
      confirmDemoteUserId = '';
      user = await getMe();
      if (user.isAdmin) {
        admins = await listAdmins();
      } else {
        admins = [];
      }
      adminPanelMessage = 'Admin demoted.';
    });
  }

  async function submitDemoteAll() {
    if (!confirmDemoteAll) {
      confirmDemoteAll = true;
      adminPanelMessage = 'Confirm demoting all other admins.';
      return;
    }
    await run(async () => {
      await demoteAllAdmins(false);
      confirmDemoteAll = false;
      admins = await listAdmins();
      adminPanelMessage = 'Other admins demoted.';
    });
  }

  async function submitCreateRoom() {
    await run(async () => {
      const details = await createRoom(createTitle, createPassword);
      await activateRoom(details, `Created ${details.room.title}.`);
      createTitle = '';
      createPassword = '';
    });
  }

  async function submitJoinRoom() {
    await run(async () => {
      const normalizedRoomId = roomIdFromInput(joinRoomId, window.location.href);
      const details = await joinRoom(normalizedRoomId, joinPassword, joinMigrationId);
      await activateRoom(details, `Joined ${details.room.title}.`);
      joinRoomId = details.room.id;
      joinPassword = '';
      joinMigrationId = '';
    });
  }

  async function autoOpenRoom(roomInput: string, migrationId: string) {
    busy = true;
    errorMessage = '';
    notice = '';
    const normalizedRoomId = roomIdFromInput(roomInput, window.location.href);
    joinRoomId = normalizedRoomId;
    joinMigrationId = migrationId;
    try {
      let details: RoomDetails;
      let joined = false;
      try {
        details = await getRoom(normalizedRoomId);
      } catch {
        details = await joinRoom(normalizedRoomId, '', migrationId);
        joined = true;
      }
      if (joined) {
        joinMigrationId = '';
        await activateRoom(details, `Joined ${details.room.title}.`);
      } else {
        await activateRoom(details, `Opened ${details.room.title}.`);
      }
    } catch (error) {
      errorMessage = messageFrom(error);
    } finally {
      busy = false;
    }
  }

  async function activateRoom(details: RoomDetails, message: string) {
    realtime?.close();
    realtime = null;
    snapshot = null;
    room = details;
    snapshot = await getRoomSnapshot(details.room.id);
    updateRoomURL(details.room.id);
    notice = message;
    try {
      await openRealtime(details.room.id);
    } catch (error) {
      connectionStatus = 'disconnected';
      errorMessage = messageFrom(error);
    }
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

  function updateRoomURL(roomId: string) {
    const url = new URL(window.location.href);
    url.searchParams.set('room', roomId);
    url.searchParams.delete('migration');
    window.history.replaceState({}, '', url);
  }

  function roomShareText(roomId: string) {
    const url = new URL(window.location.href);
    url.searchParams.set('room', roomId);
    return url.toString();
  }

  async function copyRoomLink(roomId: string) {
    const result = await copyText(roomShareText(roomId));
    if (result.copied) {
      shareFallbackText = '';
      notice = 'Room link copied.';
      return;
    }
    shareFallbackText = result.text;
    notice = 'Select the room link field and copy it manually.';
    window.setTimeout(() => {
      document.querySelector<HTMLInputElement>('[data-share-fallback]')?.select();
    });
  }

  async function openRealtime(roomId: string) {
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

        {#if user?.isAdmin}
          <div class="admin-list" aria-label="Admin membership">
            <div class="panel-title">
              <ShieldCheck size={20} />
              <h2>Admins</h2>
            </div>
            <button type="button" disabled={busy} onclick={refreshAdmins}>Refresh admins</button>
            {#each admins as admin (admin.id)}
              <article class="admin-row">
                <div>
                  <strong>{admin.displayName || admin.id}</strong>
                  <span>{new Date(admin.createdAt).toLocaleDateString()}</span>
                </div>
                <button class="danger-button" type="button" disabled={busy} onclick={() => submitDemoteAdmin(admin)}>
                  {confirmDemoteUserId === admin.id ? 'Confirm demote' : 'Demote'}
                </button>
              </article>
            {/each}
            <button class="danger-button" type="button" disabled={busy || admins.length <= 1} onclick={submitDemoteAll}>
              {confirmDemoteAll ? 'Confirm demote others' : 'Demote all others'}
            </button>
            {#if adminPanelMessage}
              <p>{adminPanelMessage}</p>
            {/if}
          </div>
        {/if}
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

    {#if snapshot}
      <Roundtable
        {snapshot}
        status={connectionStatus}
        send={(command) => {
          const sent = realtime?.send(command) ?? false;
          if (!sent) {
            errorMessage = 'Live connection is still connecting. Try again in a moment.';
          }
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
          {#if shareFallbackText}
            <label class="share-fallback">
              Room link
              <input data-share-fallback readonly value={shareFallbackText} />
            </label>
          {/if}
        </div>
        <div class="room-summary-actions">
          <button type="button" onclick={() => copyRoomLink(room.room.id)}>Copy room link</button>
          <span>{room.room.hasPassword ? 'Password protected' : 'No password'}</span>
        </div>
      </section>
    {/if}
  {/if}
</main>

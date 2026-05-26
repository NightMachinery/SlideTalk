<script lang="ts">
  import { onMount } from 'svelte';
  import CirclePlus from '@lucide/svelte/icons/circle-plus';
  import DoorOpen from '@lucide/svelte/icons/door-open';
  import KeyRound from '@lucide/svelte/icons/key-round';
  import PanelsTopLeft from '@lucide/svelte/icons/panels-top-left';
  import ShieldCheck from '@lucide/svelte/icons/shield-check';
  import UserRound from '@lucide/svelte/icons/user-round';
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
  import { connectRealtime, normalizeRoomSnapshot, type RealtimeConnection, type RealtimeEvent, type RoomSnapshot } from './lib/realtime';
  import { roomIdFromInput } from './lib/roomLink';
  import Roundtable from './lib/room/Roundtable.svelte';
  import SelectMenu from './lib/room/SelectMenu.svelte';
  import ToastList from './lib/ToastList.svelte';
  import { addToast } from './lib/toast.svelte';

  let user = $state<User | null>(null);
  let room = $state<RoomDetails | null>(null);
  let snapshot = $state<RoomSnapshot | null>(null);
  let realtime = $state<RealtimeConnection | null>(null);
  let connectionStatus = $state<'connecting' | 'connected' | 'disconnected'>('disconnected');
  let displayName = $state('');
  let profileEditing = $state(false);
  let profileStatus = $state('');
  let adminToken = $state('');
  let admins = $state<Admin[]>([]);
  let adminPanelMessage = $state('');
  let adminMessageTimer: number | null = null;
  let confirmDemoteUserId = $state('');
  let confirmDemoteAll = $state(false);
  let createTitle = $state('');
  let createPassword = $state('');
  let createMode = $state<'slides' | 'markdown' | 'audio'>('slides');
  let joinRoomId = $state('');
  let joinPassword = $state('');
  let joinMigrationId = $state('');
  let pendingRoomParam = $state('');
  let pendingMigrationParam = $state('');
  let loading = $state(true);
  let busy = $state(false);
  let notice = $state('');
  let shareFallbackText = $state('');
  let removedRoomMessage = $state('');
  let offlineCommandToastShown = false;
  const pendingUserCommandRequestIds = new Set<string>();

  const hasDisplayName = $derived(Boolean(user?.displayName.trim()));
  const needsProfile = $derived(Boolean(user && !hasDisplayName));
  const hasPendingRoomGate = $derived(Boolean(!snapshot && pendingRoomParam && needsProfile));
  const audioDriftThresholdSeconds = $derived(user?.config?.audioDriftThresholdSeconds ?? 3);

  onMount(() => {
    document.title = 'SlideTalk';
    void (async () => {
      const roomParam = new URL(window.location.href).searchParams.get('room');
      const migrationParam = new URL(window.location.href).searchParams.get('migration');
      if (roomParam) {
        joinRoomId = roomParam;
        pendingRoomParam = roomParam;
      }
      if (migrationParam) {
        joinMigrationId = migrationParam;
        pendingMigrationParam = migrationParam;
      }
      await loadProfile();
      if (roomParam && user && user.displayName.trim() !== '') {
        await autoOpenRoom(roomParam, migrationParam ?? '');
      }
    })();
    return () => {
      realtime?.close();
      realtime = null;
      pendingUserCommandRequestIds.clear();
      if (adminMessageTimer !== null) {
        window.clearTimeout(adminMessageTimer);
        adminMessageTimer = null;
      }
      document.title = 'SlideTalk';
    };
  });

  async function loadProfile() {
    loading = true;
    try {
      user = await getMe();
      displayName = user.displayName;
      profileStatus = '';
      if (user.isAdmin) {
        admins = await listAdmins();
      }
    } catch (error) {
      addToast(messageFrom(error));
    } finally {
      loading = false;
    }
  }

  async function saveProfile() {
    const nextName = displayName.trim();
    if (nextName === '') return false;
    if (nextName === user?.displayName.trim()) {
      displayName = user.displayName;
      profileEditing = false;
      profileStatus = '';
      return true;
    }

    busy = true;
    profileStatus = 'Saving...';
    try {
      user = await updateProfile(nextName);
      displayName = user.displayName;
      profileEditing = false;
      profileStatus = 'Saved';
      return true;
    } catch (error) {
      profileStatus = messageFrom(error);
      return false;
    } finally {
      busy = false;
    }
  }

  async function saveProfileAndOpenPendingRoom() {
    const saved = await saveProfile();
    if (saved && pendingRoomParam) {
      await autoOpenRoom(pendingRoomParam, pendingMigrationParam);
    }
  }

  function editProfileName() {
    displayName = user?.displayName ?? '';
    profileStatus = '';
    profileEditing = true;
    window.setTimeout(() => {
      document.querySelector<HTMLInputElement>('[data-profile-name]')?.focus();
      document.querySelector<HTMLInputElement>('[data-profile-name]')?.select();
    });
  }

  function cancelProfileEdit() {
    displayName = user?.displayName ?? '';
    profileEditing = false;
    profileStatus = '';
  }

  async function handleProfileKeydown(event: KeyboardEvent) {
    if (event.key === 'Enter') {
      event.preventDefault();
      await saveProfile();
    }
    if (event.key === 'Escape') {
      event.preventDefault();
      cancelProfileEdit();
    }
  }

  async function handleNameGateKeydown(event: KeyboardEvent) {
    if (event.key === 'Enter') {
      event.preventDefault();
      await saveProfileAndOpenPendingRoom();
    }
  }

  async function promoteAdmin() {
    await run(async () => {
      user = await submitAdminToken(adminToken);
      adminToken = '';
      admins = user.isAdmin ? await listAdmins() : [];
      notice = 'Admin access enabled.';
    });
  }

  async function refreshAdmins() {
    if (!user?.isAdmin) return;
    admins = await listAdmins();
    setAdminPanelMessage('Admin list updated.', true);
  }

  async function submitDemoteAdmin(admin: Admin) {
    if (confirmDemoteUserId !== admin.id) {
      confirmDemoteUserId = admin.id;
      setAdminPanelMessage(`Confirm demoting ${admin.displayName || admin.id}.`);
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
      setAdminPanelMessage('Admin demoted.');
    });
  }

  async function submitDemoteAll() {
    if (!confirmDemoteAll) {
      confirmDemoteAll = true;
      setAdminPanelMessage('Confirm demoting all other admins.');
      return;
    }
    await run(async () => {
      await demoteAllAdmins(false);
      confirmDemoteAll = false;
      admins = await listAdmins();
      setAdminPanelMessage('Other admins demoted.');
    });
  }

  async function submitCreateRoom() {
    await run(async () => {
      const details = await createRoom(createTitle, createPassword, createMode);
      await activateRoom(details, `Created ${details.room.title}.`);
      createTitle = '';
      createPassword = '';
      createMode = 'slides';
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
        pendingMigrationParam = '';
        await activateRoom(details, `Joined ${details.room.title}.`);
      } else {
        await activateRoom(details, '');
      }
    } catch (error) {
      addToast(messageFrom(error));
    } finally {
      busy = false;
    }
  }

  async function activateRoom(details: RoomDetails, message: string) {
    realtime?.close();
    realtime = null;
    pendingUserCommandRequestIds.clear();
    offlineCommandToastShown = false;
    snapshot = null;
    removedRoomMessage = '';
    room = details;
    snapshot = normalizeRoomSnapshot(await getRoomSnapshot(details.room.id));
    updateRoomURL(details.room.id);
    pendingRoomParam = '';
    pendingMigrationParam = '';
    document.title = details.room.title || 'SlideTalk';
    notice = message;
    try {
      await openRealtime(details.room.id);
    } catch (error) {
      connectionStatus = 'disconnected';
      addToast(messageFrom(error));
    }
  }

  async function run(task: () => Promise<void>) {
    busy = true;
    notice = '';
    try {
      await task();
    } catch (error) {
      addToast(messageFrom(error));
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

  function clearRoomURL() {
    const url = new URL(window.location.href);
    url.searchParams.delete('room');
    url.searchParams.delete('migration');
    window.history.replaceState({}, '', `${url.pathname}${url.search}${url.hash}`);
  }

  function setAdminPanelMessage(message: string, autoHide = false) {
    if (adminMessageTimer !== null) {
      window.clearTimeout(adminMessageTimer);
      adminMessageTimer = null;
    }
    adminPanelMessage = message;
    if (autoHide) {
      adminMessageTimer = window.setTimeout(() => {
        if (adminPanelMessage === message) {
          adminPanelMessage = '';
        }
        adminMessageTimer = null;
      }, 3000);
    }
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
        const wasPendingUserCommand = event.requestId ? pendingUserCommandRequestIds.delete(event.requestId) : false;
        if (event.type === 'room.snapshot' && event.payload) {
          snapshot = event.payload;
        }
        if (event.type === 'room.kicked') {
          realtime?.close();
          realtime = null;
          connectionStatus = 'disconnected';
          snapshot = null;
          room = null;
          removedRoomMessage = event.message || "You've been removed from that room.";
          notice = '';
          pendingRoomParam = '';
          pendingMigrationParam = '';
          clearRoomURL();
          document.title = 'SlideTalk';
        }
        if (event.type === 'error') {
          if (wasPendingUserCommand) {
            addToast(event.message ?? 'Realtime command failed.');
          }
        }
      },
      (status) => {
        connectionStatus = status;
        if (status === 'connected') {
          offlineCommandToastShown = false;
        }
      }
    );
  }

  function sendRealtimeCommand(command: RealtimeCommand, options: { notifyOnFailure?: boolean } = {}) {
    const notifyOnFailure = options.notifyOnFailure ?? true;
    const requestId = realtime?.send(command) ?? null;
    if (requestId) {
      if (notifyOnFailure) {
        pendingUserCommandRequestIds.add(requestId);
      }
      return;
    }
    if (notifyOnFailure && !offlineCommandToastShown) {
      offlineCommandToastShown = true;
      addToast('Live connection is offline. Changes will work after it reconnects.');
    }
  }
</script>

<svelte:head>
  <meta
    name="description"
    content="SlideTalk coordinates roundtable speaking order, slides, and timers for online discussions."
  />
</svelte:head>

<main class:active-room={snapshot} class="app-shell">
  {#if loading}
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
    <section class="loading-panel" aria-live="polite">Loading local identity...</section>
  {:else if removedRoomMessage}
    <section class="removed-room" aria-labelledby="removed-room-title">
      <DoorOpen size={34} />
      <h1 id="removed-room-title">You've been removed from that room</h1>
      <p>{removedRoomMessage}</p>
      <button type="button" onclick={() => (removedRoomMessage = '')}>Return home</button>
    </section>
  {:else if snapshot}
    {#if notice}
      <p class="feedback notice room-feedback" role="status">{notice}</p>
    {/if}
      <Roundtable
        {snapshot}
        status={connectionStatus}
        {audioDriftThresholdSeconds}
        send={sendRealtimeCommand}
    />
  {:else}
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

    {#if hasPendingRoomGate}
      <section class="name-gate" aria-labelledby="name-gate-title">
        <p class="kicker">Join room</p>
        <h1 id="name-gate-title">Choose a display name</h1>
        <form class="name-gate-form" onsubmit={(event) => { event.preventDefault(); saveProfileAndOpenPendingRoom(); }}>
          <label>
            Display name
            <input
              bind:value={displayName}
              data-profile-name
              maxlength="80"
              autocomplete="name"
              placeholder="Ada"
              onkeydown={handleNameGateKeydown}
            />
          </label>
          <button type="submit" disabled={busy || displayName.trim() === ''}>Continue</button>
        </form>
        {#if profileStatus}
          <p class="inline-status" role="status">{profileStatus}</p>
        {/if}
      </section>
    {:else}
      <section class="workspace" aria-labelledby="workspace-title">
        <div class="intro">
          <p class="kicker">Room control</p>
          <h1 id="workspace-title">Open a roundtable room.</h1>
          <p class="summary">Create a room, share the link, keep the conversation moving.</p>
        </div>

        <aside class="profile-panel" aria-label="Profile settings">
          <div class="profile-line">
            <div class="panel-title">
              <UserRound size={18} />
              <h2>Profile</h2>
            </div>
            {#if profileEditing || needsProfile}
              <label class="compact-label">
                Display name
                <input
                  bind:value={displayName}
                  data-profile-name
                  maxlength="80"
                  autocomplete="name"
                  placeholder="Ada"
                  onblur={saveProfile}
                  onkeydown={handleProfileKeydown}
                />
              </label>
            {:else}
              <button class="profile-name-button" type="button" onclick={editProfileName}>
                <span>Display name</span>
                <strong>{user?.displayName}</strong>
              </button>
            {/if}
            {#if profileStatus}
              <p class="inline-status" role="status">{profileStatus}</p>
            {/if}
          </div>

          <details class="admin-disclosure">
            <summary>
              <span class="panel-title">
                <ShieldCheck size={18} />
                <span>Admin</span>
              </span>
              <span>{user?.isAdmin ? 'Manage' : 'Optional'}</span>
            </summary>

            {#if user?.isAdmin}
              <div class="admin-list" aria-label="Admin membership">
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
              </div>
            {:else}
              <div class="admin-box">
                <div class="panel-title">
                  <ShieldCheck size={18} />
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
            {/if}

            {#if adminPanelMessage}
              <p class="admin-message" role="status">{adminPanelMessage}</p>
            {/if}
          </details>
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
          <SelectMenu
            label="Starting mode"
            value={createMode}
            disabled={needsProfile}
            options={[
              { value: 'slides', label: 'Slides' },
              { value: 'markdown', label: 'Markdown' },
              { value: 'audio', label: 'Audio' }
            ]}
            onChange={(value) => (createMode = value as 'slides' | 'markdown' | 'audio')}
          />
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
    {/if}

    {#if notice}
      <p class="feedback notice" role="status">{notice}</p>
    {/if}
    {#if needsProfile && !hasPendingRoomGate}
      <p class="feedback notice" role="status">Save a display name before creating or joining rooms.</p>
    {/if}

    {#if room}
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

<ToastList />

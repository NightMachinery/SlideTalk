<script lang="ts">
  import { Eye, LogOut, Shield, UserRound, UsersRound } from '@lucide/svelte';
  import type { RealtimeCommand, RoomSnapshot, SnapshotMember } from '../realtime';

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

  const isMod = $derived(snapshot.caller.role === 'mod');

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
</script>

<section class="roundtable" aria-label="Live roundtable">
  <div class="room-board">
    <div class="room-board-header">
      <div>
        <p class="kicker">Live room</p>
        <h2>{snapshot.room.title}</h2>
      </div>
      <span class={['connection-pill', status]}>{status}</span>
    </div>

    <div class="speaker-ledger">
      <button class="list-toggle" type="button" onclick={() => (participantsCollapsed = !participantsCollapsed)}>
        <UsersRound size={19} />
        <span>Participants</span>
        <strong>{snapshot.participants.length}</strong>
      </button>
      {#if !participantsCollapsed}
        <div class="member-list">
          {#each snapshot.participants as member, index (member.userId)}
            <article class="member-row">
              <div class="member-identity">
                <UserRound size={18} />
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


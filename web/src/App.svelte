<script lang="ts">
  import { CirclePlus, PanelsTopLeft, UsersRound } from '@lucide/svelte';

  const roomActions = [
    {
      title: 'Create a room',
      description: 'Start a moderated discussion with a stable speaking order.',
      icon: CirclePlus
    },
    {
      title: 'Join by link',
      description: 'Open a shared room URL and keep your browser identity.',
      icon: UsersRound
    }
  ];
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
    <div class="topbar-actions" aria-label="Application status">
      <span class="status-dot" aria-hidden="true"></span>
      <span>Local first seed</span>
    </div>
  </nav>

  <section class="workspace" aria-labelledby="workspace-title">
    <div class="intro">
      <p class="kicker">Roundtable coordination</p>
      <h1 id="workspace-title">Keep turns, slides, and shared timing in one quiet room.</h1>
      <p class="summary">
        Audio stays wherever your group already meets. SlideTalk keeps the discussion order,
        observer queue, moderator actions, and presentation state visible to everyone.
      </p>
    </div>

    <div class="action-panel" aria-label="Room actions">
      {#each roomActions as action (action.title)}
        {@const Icon = action.icon}
        <article class="action-card">
          <div class="icon-box" aria-hidden="true">
            <Icon size={24} strokeWidth={2.1} />
          </div>
          <div>
            <h2>{action.title}</h2>
            <p>{action.description}</p>
          </div>
        </article>
      {/each}
    </div>
  </section>

  <section class="empty-state" aria-label="Room list">
    <div>
      <h2>No rooms yet</h2>
      <p>Room creation lands in the next milestone. This shell verifies the Go, Vite, Svelte, and icon stack.</p>
    </div>
    <button type="button">Create room</button>
  </section>
</main>

## Vite + Lucide Build Performance

When optimizing Vite/Svelte builds that use Lucide icons, do not assume named imports from the package root are build-time cheap.

Avoid package barrel imports such as:

```ts
import { Copy, Power, Smartphone } from 'lucide-svelte';
import { Download, Save } from '@lucide/svelte';
```

Even when tree-shaking removes unused icons from the final bundle, Vite/Rollup may still need to parse and analyze the package barrel during production builds. This can noticeably slow `vite build`.

Prefer direct per-icon imports:

```ts
import Copy from 'lucide-svelte/icons/copy';
import Power from 'lucide-svelte/icons/power';
import Smartphone from 'lucide-svelte/icons/smartphone';
```

Or, if the repo uses `@lucide/svelte`:

```ts
import Copy from '@lucide/svelte/icons/copy';
import Power from '@lucide/svelte/icons/power';
import Smartphone from '@lucide/svelte/icons/smartphone';
```

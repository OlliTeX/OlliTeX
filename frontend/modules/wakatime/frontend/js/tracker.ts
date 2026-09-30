// WakaTime tracker for OlliTeX (candidate F).
// Port of services/web/modules/wakatime/tracker.ts (reference
// wakatime PR#249): 2-minute per-file throttle, debounced flush on idle,
// heartbeats relayed through the Go web (which carries the user's key —
// the browser never touches wakatime.com or the key).

type HeartbeatRecord = {
  entity: string;
  time: number;
  isWrite: boolean;
};

const THROTTLE_MS = 2 * 60 * 1000; // reference: 2 minutes per file

const pending = new Map<string, HeartbeatRecord>();
let flushTimer: ReturnType<typeof setTimeout> | null = null;
let flushing = false;
const FLUSH_DELAY_MS = 10_000; // reference: queue flush after 10s idle

function getMeta(name: string): string | null {
  return document.head.querySelector(`meta[name="${name}"]`)?.getAttribute('content') ?? null;
}

function projectApiBase(): string | null {
  // Editor page meta slot (68-slot contract) — ol-project_id.
  const pid = getMeta('ol-project_id');
  return pid ? `/project/${pid}/wakatime` : null;
}

function currentEntity(): string {
  // Best-effort active file: the open-documents bar marks the active tab
  // (class 'active'); fallback 'main.tex' (WakaTime still attributes the
  // time to the project — reference entity precision is a nice-to-have).
  const tab = document.querySelector('[data-testid="open-document"][aria-current="page"]')
    ?? document.querySelector('.open-document.active, .cdk-tab[aria-selected="true"] [data-file-name]');
  const name = (tab as HTMLElement | null)?.getAttribute('data-file-name')
    ?? (tab as HTMLElement | null)?.getAttribute('content')
    ?? (tab as HTMLElement | null)?.textContent?.trim();
  return name || 'main.tex';
}

export async function sendHeartbeat() {
  const api = projectApiBase();
  if (!api) {
    return;
  }
  const entity = currentEntity();
  const now = Math.floor(Date.now() / 1000);
  const existing = pending.get(entity);
  if (existing && now - existing.time < THROTTLE_MS / 1000) {
    // refresh only; the 2-minute throttle is per-file (reference parity)
    return;
  }
  pending.set(entity, { entity, time: now, isWrite: true });
  scheduleFlush();
}

function scheduleFlush() {
  if (flushTimer) {
    clearTimeout(flushTimer);
  }
  flushTimer = setTimeout(flush, FLUSH_DELAY_MS);
}

async function flush() {
  if (flushing || pending.size === 0) {
    return;
  }
  flushing = true
  try {
    const hbs = [...pending.values()];
    pending.clear();
    const api = projectApiBase();
    if (!api || hbs.length === 0) {
      return;
    }
    const useBulk = hbs.length > 1;
    const url = useBulk ? `${api}/heartbeats/bulk` : `${api}/heartbeat`;
    const body = useBulk ? hbs : hbs[0];
    // Graceful degradation: any failure (air-gapped instance, WakaTime
    // down, not linked) is swallowed — the editor is never blocked
    // (reference behaviour; failures are visible in the server logs).
    await fetch(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      credentials: 'same-origin',
      body: JSON.stringify(body),
    }).catch(() => undefined);
  } finally {
    flushing = false;
  }
}

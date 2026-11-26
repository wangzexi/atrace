// The server tells the page what changed; the page asks for what it shows. A signal carries no data.
//   entries     what the timeline or a post shows
//   collectors  the collector's connection, its runs and its platforms (the owner only)

export type Change = 'entries' | 'collectors'

const CHANGES: Change[] = ['entries', 'collectors']
const listeners: Record<Change, Set<() => void>> = { entries: new Set(), collectors: new Set() }
let source: EventSource | null = null
let lost = false

function connect() {
  source = new EventSource('/api/events')
  for (const change of CHANGES)
    source.addEventListener(change, () => listeners[change].forEach((listener) => listener()))
  // The browser reconnects by itself; what changed while it was away is not known, so everything is asked again.
  source.onerror = () => {
    lost = true
  }
  source.onopen = () => {
    if (!lost) return
    lost = false
    for (const change of CHANGES) listeners[change].forEach((listener) => listener())
  }
}

// subscribe calls fn after a change of this kind, once for a burst: wait ms after the last signal, but
// never later than maxWait ms after the first. It returns what ends the subscription.
export function subscribe(change: Change, fn: () => void, wait = 1500, maxWait = 6000): () => void {
  let timer: number | undefined
  let first = 0
  const listener = () => {
    const now = Date.now()
    if (!first) first = now
    window.clearTimeout(timer)
    timer = window.setTimeout(
      () => {
        first = 0
        fn()
      },
      Math.max(0, Math.min(wait, first + maxWait - now)),
    )
  }
  listeners[change].add(listener)
  if (!source) connect()
  return () => {
    window.clearTimeout(timer)
    listeners[change].delete(listener)
    if (CHANGES.every((kind) => listeners[kind].size === 0)) {
      source?.close()
      source = null
    }
  }
}

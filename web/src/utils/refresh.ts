/**
 * Shared refresh primitives.
 *
 * Lists in this app are refreshed either by a background poll or by an
 * explicit user action. Replacing a whole array on every poll makes Vue
 * re-evaluate every row and, when rows contain conditional buttons, visibly
 * add/remove DOM nodes. These helpers keep the DOM stable: only fields that
 * actually changed are written, and the array reference is kept whenever the
 * id set is unchanged.
 */

export interface RefreshOptions {
  /** Explicit user refreshes may legitimately clear the list; polls may not. */
  force?: boolean
}

function different(a: unknown, b: unknown): boolean {
  if (a === b) return false
  if (typeof a !== typeof b) return true
  if (a && b && typeof a === 'object') {
    try {
      return JSON.stringify(a) !== JSON.stringify(b)
    } catch {
      return true
    }
  }
  return true
}

/**
 * Merge `incoming` into `current` by id.
 * - Existing rows are mutated in place, but only for fields that changed.
 * - The array reference is preserved when the id set and order are unchanged.
 * - A new array is returned only when rows were added, removed or reordered.
 * - Background polls that return an empty list keep the current rows.
 */
export function mergeById<T extends { id: number }>(
  current: T[],
  incoming: T[],
  opts: RefreshOptions = {},
): T[] {
  const { force = true } = opts
  const next = incoming || []
  if (!force && next.length === 0 && current.length > 0) return current

  const index = new Map<number, number>()
  current.forEach((item, i) => index.set(item.id, i))

  for (const item of next) {
    const i = index.get(item.id)
    if (i === undefined) continue
    const old = current[i]
    for (const key of Object.keys(item) as (keyof T)[]) {
      if (different(old[key], item[key])) old[key] = item[key]
    }
  }

  const sameSet =
    next.length === current.length && next.every((item, i) => current[i].id === item.id)
  if (sameSet) return current

  return next.map((item) => {
    const i = index.get(item.id)
    return i === undefined ? item : current[i]
  })
}

export interface PollerOptions {
  interval?: number
  /** When provided, the poller only runs while this returns true. */
  activeWhen?: () => boolean
  /** Pause while a dialog is open, etc. */
  pauseWhen?: () => boolean
  /** After N consecutive failures, only try every M ticks. */
  backoffAfter?: number
  backoffEvery?: number
}

export interface Poller {
  start(): void
  stop(): void
  /** Re-evaluate activeWhen after data changes. */
  sync(): void
  /** Explicit refresh: force=true, errors are surfaced by the task itself. */
  refresh(): void
}

export interface TaskOptions {
  /** True for background polls: failures must stay silent. */
  quiet: boolean
  /** True for explicit refreshes: empty responses are accepted. */
  force: boolean
}

/**
 * A poll scheduler with the guards that every page needs: skip hidden tabs,
 * pause while dialogs are open, deduplicate slow requests, back off after
 * repeated failures and refresh once when the tab becomes visible again.
 */
export function createPoller(
  task: (opts: TaskOptions) => void | Promise<void>,
  options: PollerOptions = {},
): Poller {
  const {
    interval = 5000,
    activeWhen,
    pauseWhen,
    backoffAfter = 3,
    backoffEvery = 4,
  } = options
  let timer: number | undefined
  let inFlight = false
  let failures = 0
  let ticks = 0
  let started = false

  const run = async (opts: TaskOptions) => {
    if (inFlight) return
    inFlight = true
    try {
      await task(opts)
      failures = 0
    } catch {
      failures++
    } finally {
      inFlight = false
    }
  }

  const tick = () => {
    if (typeof document !== 'undefined' && document.visibilityState !== 'visible') return
    if (pauseWhen?.()) return
    ticks++
    if (failures >= backoffAfter && ticks % backoffEvery !== 0) return
    void run({ quiet: true, force: false })
  }

  const onVisibility = () => {
    if (document.visibilityState === 'visible') void run({ quiet: true, force: false })
  }

  const sync = () => {
    const shouldRun = started && (activeWhen ? activeWhen() : true)
    if (shouldRun && !timer) timer = window.setInterval(tick, interval)
    if (!shouldRun && timer) {
      clearInterval(timer)
      timer = undefined
    }
  }

  return {
    start() {
      if (started) return
      started = true
      document.addEventListener('visibilitychange', onVisibility)
      sync()
    },
    stop() {
      started = false
      if (timer) {
        clearInterval(timer)
        timer = undefined
      }
      document.removeEventListener('visibilitychange', onVisibility)
    },
    sync,
    refresh() {
      void run({ quiet: false, force: true })
    },
  }
}

/**
 * Merge a single object response in place. Returns the same reference whenever
 * possible so bound form fields do not lose focus or re-render needlessly.
 */
export function mergeObject<T extends object>(current: T | null, incoming: T): T {
  if (!current) return incoming
  for (const key of Object.keys(incoming) as (keyof T)[]) {
    if (different(current[key], incoming[key])) current[key] = incoming[key]
  }
  return current
}

/** Replace an array only when its contents actually changed. */
export function mergeArray<T>(current: T[], incoming: T[], opts: RefreshOptions = {}): T[] {
  const { force = true } = opts
  const next = incoming || []
  if (!force && next.length === 0 && current.length > 0) return current
  if (next.length === current.length && next.every((item, i) => !different(item, current[i]))) {
    return current
  }
  return next
}

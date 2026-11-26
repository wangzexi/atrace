// The timeline list: entries, paging, type filter, debounced search and the sentinel that loads more.
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
  fetchEntries,
  readEntry,
  KINDS,
  type Entry as EntryData,
  type Kind,
  type Visibility,
} from '../api'
import { subscribe } from '../events'

// The filters survive a reload of the tab: they live in sessionStorage, which is per tab.
const FILTERS_KEY = 'atrace.filters'
// The type filter is a single choice: one type (原创 by default), or 全部.
export type Choice = Kind | 'all'
export const DEFAULT_CHOICE: Choice = 'Create'
const ALL_KINDS = KINDS.map((item) => item.kind)
// Whose timeline it is: the owner's own (id 0), or a person's, shown under their name.
export interface Whose {
  id: number
  name: string
}
const MINE: Whose = { id: 0, name: '' }
interface Filters {
  choice: Choice
  visibility: Visibility
  query: string
  regex: boolean
  person: Whose
}

function savedFilters(): Filters {
  try {
    const saved = JSON.parse(sessionStorage.getItem(FILTERS_KEY) ?? '{}')
    const choice: Choice =
      saved.choice === 'all' || ALL_KINDS.includes(saved.choice) ? saved.choice : DEFAULT_CHOICE
    const person: Whose =
      Number.isInteger(saved.person?.id) &&
      saved.person.id > 0 &&
      typeof saved.person.name === 'string'
        ? saved.person
        : MINE
    return {
      choice,
      visibility:
        saved.visibility === 'public' || saved.visibility === 'private' ? saved.visibility : 'all',
      query: typeof saved.query === 'string' ? saved.query : '',
      regex: saved.regex === true,
      person,
    }
  } catch {
    return { choice: DEFAULT_CHOICE, visibility: 'all', query: '', regex: false, person: MINE }
  }
}

// Watch the sentinel again when navigation returns to the list.
export function useTimeline(
  path: string,
  owner: boolean,
  setOwner: React.Dispatch<React.SetStateAction<boolean>>,
  sessionVersion: number,
) {
  const [initial] = useState(savedFilters)
  const [entries, setEntries] = useState<EntryData[]>([])
  const [next, setNext] = useState('')
  const [more, setMore] = useState(true)
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)
  const [query, setQuery] = useState(initial.query)
  const [regex, setRegex] = useState(initial.regex)
  const [choice, setChoice] = useState<Choice>(initial.choice)
  const [visibility, setVisibility] = useState<Visibility>(initial.visibility)
  const [person, setPerson] = useState<Whose>(initial.person)
  // What the list is searched for: the typed words, after a short pause in typing.
  const [applied, setApplied] = useState({
    text: initial.query.trim(),
    regex: initial.regex && initial.query.trim() !== '',
  })
  const kinds = useMemo<Kind[]>(() => (choice === 'all' ? ALL_KINDS : [choice]), [choice])
  const generation = useRef(0)
  const pageRequest = useRef<AbortController | null>(null)
  const refreshRequest = useRef<AbortController | null>(null)
  useEffect(
    () => () => {
      pageRequest.current?.abort()
      refreshRequest.current?.abort()
    },
    [],
  )
  const sentinel = useRef<HTMLDivElement>(null)
  // How many entries on the server are newer than the top of the list, which is not shown until asked for.
  const [pending, setPending] = useState(0)
  const entriesRef = useRef<EntryData[]>([])
  entriesRef.current = entries

  // The list starts over from its first page: whatever was loading is let go, what was loaded is dropped.
  const restart = useCallback(() => {
    generation.current += 1
    pageRequest.current?.abort()
    pageRequest.current = null
    refreshRequest.current?.abort()
    setEntries([])
    setNext('')
    setMore(true)
    setError('')
    setLoading(false)
    setPending(0)
  }, [])

  const load = useCallback(async () => {
    if (pageRequest.current || !more) return
    const request = new AbortController()
    pageRequest.current = request
    const mine = generation.current
    setLoading(true)
    try {
      const page = await fetchEntries(
        next,
        kinds,
        applied.text,
        applied.regex,
        person.id,
        visibility,
        request.signal,
      )
      if (request.signal.aborted || mine !== generation.current) return // the filter changed while this was loading
      if (!page.owner && visibility !== 'all') {
        setOwner(false)
        setVisibility('all')
        restart()
        return
      }
      setEntries((current) => [...current, ...page.entries])
      setNext(page.next)
      setMore(page.more)
      setOwner(page.owner)
      setError('')
    } catch (e) {
      if (!request.signal.aborted && mine === generation.current) setError((e as Error).message)
    } finally {
      if (pageRequest.current === request) {
        pageRequest.current = null
        setLoading(false)
      }
    }
  }, [more, next, kinds, applied, person.id, setOwner, visibility, restart])

  useEffect(() => {
    try {
      sessionStorage.setItem(
        FILTERS_KEY,
        JSON.stringify({ choice, visibility, query, regex, person }),
      )
    } catch {
      // storage can be unavailable (private windows); the filters just do not survive a reload
    }
  }, [choice, visibility, query, regex, person])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      const text = query.trim()
      const wanted = { text, regex: regex && text !== '' }
      setApplied((now) => (now.text === wanted.text && now.regex === wanted.regex ? now : wanted))
    }, 300)
    return () => window.clearTimeout(timer)
  }, [query, regex])

  const previousSession = useRef(sessionVersion)
  useEffect(() => {
    if (previousSession.current === sessionVersion) return
    previousSession.current = sessionVersion
    restart()
  }, [sessionVersion, restart])

  // A new search starts the timeline over; the server pages through its matches.
  const firstSearch = useRef(true)
  useEffect(() => {
    if (firstSearch.current) {
      firstSearch.current = false
      return
    }
    restart()
  }, [applied, restart])

  // Choosing other types starts the timeline over: the server filters, so paging stays correct.
  function choose(next: Choice) {
    if (next === choice) return
    setChoice(next)
    restart()
  }

  // Looking at a person starts the timeline over with everything they did (a comment is not an original post);
  // going back to one's own starts it over with the usual choice.
  function chooseVisibility(next: Visibility) {
    if (next === visibility) return
    setVisibility(next)
    restart()
  }

  function viewPerson(next: Whose) {
    setPerson(next)
    setChoice(next.id ? 'all' : DEFAULT_CHOICE)
    restart()
  }

  // Load the next page when the end of the list scrolls near.
  useEffect(() => {
    const el = sentinel.current
    if (!el || error) return
    const observer = new IntersectionObserver(
      (hits) => hits.some((hit) => hit.isIntersecting) && load(),
      { rootMargin: '300px' },
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [load, error, entries.length, path])

  // The new entries are loaded when asked for, as on X, so the list does not move under the reader.
  const showNew = useCallback(() => {
    restart()
    window.scrollTo(0, 0)
  }, [restart])

  // The server says something changed. Count what is new above the list, and read again what is in view.
  const refreshNow = useCallback(async () => {
    if (pageRequest.current && entriesRef.current.length === 0) return
    refreshRequest.current?.abort()
    const request = new AbortController()
    refreshRequest.current = request
    const mine = generation.current
    let page
    try {
      page = await fetchEntries(
        '',
        kinds,
        applied.text,
        applied.regex,
        person.id,
        visibility,
        request.signal,
      )
    } catch {
      return
    }
    const loaded = entriesRef.current
    if (request.signal.aborted || mine !== generation.current) return
    const known = new Set(loaded.map((entry) => entry.ref))
    const top = page.entries.findIndex((entry) => entry.ref === loaded[0]?.ref)
    setPending(top >= 0 ? top : page.entries.filter((entry) => !known.has(entry.ref)).length)
    const fresh = new Map(page.entries.map((entry) => [entry.ref, entry]))
    const missing = viewportRefs().filter((ref) => known.has(ref) && !fresh.has(ref))
    const more = await Promise.all(
      missing.map((ref) => readEntry(ref, request.signal).catch(() => null)),
    )
    if (request.signal.aborted || mine !== generation.current) return
    for (const entry of more) if (entry) fresh.set(entry.ref, entry)
    setEntries((current) => {
      let changed = false
      const next = current.map((entry) => {
        const now = fresh.get(entry.ref)
        if (!now || JSON.stringify(now) === JSON.stringify(entry)) return entry
        changed = true
        return now
      })
      return changed ? next.filter((entry) => matchesVisibility(entry, visibility)) : current
    })
  }, [kinds, applied, person.id, visibility])
  const refreshRef = useRef(refreshNow)
  refreshRef.current = refreshNow
  useEffect(
    () => (path === '/' ? subscribe('entries', () => void refreshRef.current()) : undefined),
    [path],
  )

  // The list is filtered when it is not the default one: 原创 with no search is how it starts.
  const filtered = choice !== DEFAULT_CHOICE || visibility !== 'all' || query.trim() !== '' || regex

  return {
    entries,
    setEntries,
    setNext,
    more,
    error,
    setError,
    loading,
    query,
    setQuery,
    regex,
    setRegex,
    choice,
    setChoice,
    choose,
    visibility,
    chooseVisibility,
    person,
    viewPerson,
    applied,
    owner,
    setOwner,
    sentinel,
    load,
    filtered,
    pending,
    showNew,
  }
}

// The entries on the screen, by ref.
function viewportRefs(): string[] {
  return [...document.querySelectorAll<HTMLElement>('article.entry[data-ref]')]
    .filter((el) => {
      const box = el.getBoundingClientRect()
      return box.bottom > 0 && box.top < window.innerHeight
    })
    .map((el) => el.dataset.ref ?? '')
}

export function matchesVisibility(
  entry: Pick<EntryData, 'public'>,
  visibility: Visibility,
): boolean {
  return visibility === 'all' || entry.public === (visibility === 'public')
}

import { ArrowUpRight, Settings as SettingsIcon } from 'lucide-react'
import { useCallback, useEffect, useRef, useState } from 'react'
import { LOAD_FAILED, personOfAccount, readEntry, wantPost, type Entry as EntryData } from '../api'
import { subscribe } from '../events'
import ActivityCard from './ActivityCard/ActivityCard'
import Share from './Share'
import { useEntryActions } from './useEntryActions'
import { DEFAULT_CHOICE, useTimeline } from './useTimeline'
import PageHeader from '../PageHeader'
import Filter from './Filter'
import ImageViewer from './ImageViewer'

export default function Timeline({
  path,
  postId,
  active,
  title,
  owner,
  setOwner,
  sessionVersion,
  onNavigate,
  onBack,
  onTitleClick,
}: {
  path: string
  postId: string | null
  active: boolean
  title: string
  owner: boolean
  setOwner: React.Dispatch<React.SetStateAction<boolean>>
  sessionVersion: number
  onNavigate: (to: string, replace?: boolean) => void
  onBack: () => void
  onTitleClick: (event: React.MouseEvent) => boolean
}) {
  const subpage = postId !== null
  const markdownHref = postId ? `/${postId}.md` : '/index.md'
  const [detail, setDetail] = useState<EntryData | null>(null)
  const [detailError, setDetailError] = useState('')
  const scrollBefore = useRef(0)
  const timeline = useTimeline(path, owner, setOwner, sessionVersion)
  const {
    entries,
    more,
    error,
    loading,
    setQuery,
    setRegex,
    choose,
    chooseVisibility,
    person,
    viewPerson,
    applied,
    sentinel,
    load,
    filtered,
    pending,
    showNew,
  } = timeline
  // The picture viewer: the pictures of one post and the one on show.
  const [viewing, setViewing] = useState<{ images: string[]; index: number } | null>(null)
  const {
    remove,
    restore,
    undo,
    notice,
    deleteComment,
    togglePublic,
    togglePublicComment,
    sharing,
    setSharing,
    replace,
  } = useEntryActions(timeline, setDetail, postId, onBack)

  const navigate = useCallback(
    (to: string, replace = false) => {
      if (path === '/') scrollBefore.current = window.scrollY
      onNavigate(to, replace)
    },
    [path, onNavigate],
  )

  // Opening a post changes the URL; going back restores the list where it was.
  function open(entry: EntryData) {
    setDetail(entry)
    setDetailError('')
    navigate(`/${entry.ref}`)
  }

  // Browsing a name never creates or changes a configured person.
  async function openPerson(account: string, name: string) {
    try {
      const id = await personOfAccount(account)
      if (!id) return
      viewPerson({ id, name })
      if (path !== '/') navigate('/')
      else window.scrollTo(0, 0)
    } catch {
      /* the owner stays where they are; the name can be clicked again */
    }
  }

  // Clicking the title goes one step towards a clean home: from a post to the list, then up to the
  // top of the list, then the filter is cleared.
  function titleClick(event: React.MouseEvent) {
    if ((event.target as HTMLElement).closest('button, a, form, input, .filter')) return
    if (path === '/') scrollBefore.current = window.scrollY
    if (onTitleClick(event)) return
    if (person.id) {
      viewPerson({ id: 0, name: '' })
    } else if (window.scrollY > 0) {
      window.scrollTo({ top: 0, behavior: 'smooth' })
    } else if (filtered) {
      choose(DEFAULT_CHOICE)
      chooseVisibility('all')
      setQuery('')
      setRegex(false)
    }
  }

  // Going back to the list returns the reader to where they were. The list can grow after it shows (pictures and
  // fonts load), so the position is held until the reader scrolls themselves or two seconds pass.
  useEffect(() => {
    if (path !== '/') return
    const target = scrollBefore.current
    const hold = () => {
      if (window.scrollY !== target) window.scrollTo(0, target)
    }
    const observer = new ResizeObserver(hold)
    observer.observe(document.body)
    const events = ['wheel', 'touchstart', 'keydown', 'pointerdown'] as const
    const release = () => {
      observer.disconnect()
      window.clearTimeout(timer)
      for (const name of events) window.removeEventListener(name, release)
    }
    const timer = window.setTimeout(release, 2000)
    for (const name of events) window.addEventListener(name, release, { passive: true })
    requestAnimationFrame(hold)
    return release
  }, [path])

  useEffect(() => {
    if (postId === null) return
    const controller = new AbortController()
    const { signal } = controller
    let unsubscribe: (() => void) | undefined
    setDetailError('')
    setDetail((now) =>
      now?.ref === postId ? now : (entries.find((item) => item.ref === postId) ?? null),
    )
    void (async () => {
      let initial
      try {
        initial = await readEntry(postId, signal)
      } catch {
        if (!signal.aborted) setDetailError('这条内容不存在，或已被删除。')
        return
      }
      if (signal.aborted) return
      setDetail(initial)
      setOwner(initial.activityId > 0)
      if (initial.activityId > 0) wantPost(initial.ref)
      // The post shown is read again when the server says something changed, and replaced if it is different.
      unsubscribe = subscribe(
        'entries',
        () => {
          void readEntry(postId, signal)
            .then((next) =>
              setDetail((now) => (JSON.stringify(now) === JSON.stringify(next) ? now : next)),
            )
            .catch(() => {})
        },
        500,
        3000,
      )
    })()
    return () => {
      controller.abort()
      unsubscribe?.()
    }
    // entries is only a first guess for what to show while the post loads
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [path, postId])

  function step(by: number) {
    setViewing((now) =>
      now ? { ...now, index: (now.index + by + now.images.length) % now.images.length } : now,
    )
  }

  if (!active) return null
  return (
    <>
      <PageHeader title={title} subpage={subpage} onBack={onBack} onTitleClick={titleClick}>
        {!subpage && <Filter timeline={timeline} />}
        <span className="intro-spacer" aria-hidden="true" />
        {markdownHref && (
          <a
            className="md-link"
            href={markdownHref}
            target="_blank"
            rel="noopener"
            aria-label="Markdown 版本，适合 AI 阅读"
          >
            Markdown <ArrowUpRight size={13} aria-hidden="true" />
          </a>
        )}
        {owner && (
          <a
            className="filter-button"
            aria-label="设置"
            data-tip="设置"
            href={'/settings/'}
            onClick={(event) => {
              if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return
              event.preventDefault()
              navigate('/settings/')
            }}
          >
            <SettingsIcon aria-hidden="true" strokeWidth={2} />
          </a>
        )}
      </PageHeader>

      {postId === null ? (
        <>
          <section className="feed" aria-label="个人时间线">
            {pending > 0 && (
              <button className="feed-new" type="button" onClick={showNew}>
                显示 {pending} 条新内容
              </button>
            )}
            {entries.map((entry) => (
              <ActivityCard
                key={entry.ref}
                entry={entry}
                onImage={(images, index) => setViewing({ images, index })}
                onDelete={remove}
                onPublic={togglePublic}
                onShare={setSharing}
                onDeleteComment={deleteComment}
                onPublicComment={togglePublicComment}
                onPerson={owner ? openPerson : undefined}
                onOpen={open}
              />
            ))}
            {!entries.length && !more && !loading && !error && (
              <p className="feed-status">
                {applied.text
                  ? '没有找到匹配的内容。'
                  : owner
                    ? '没有符合条件的内容。'
                    : '还没有公开的内容。'}
              </p>
            )}
            <div ref={sentinel} />
            {more && (
              <button className="feed-more" type="button" disabled={loading} onClick={load}>
                {loading ? '加载中…' : '加载更早'}
              </button>
            )}
            {error && error !== LOAD_FAILED && (
              <p className="feed-status" role="status">
                {error}
              </p>
            )}
          </section>
        </>
      ) : (
        <>
          <section className="feed" aria-label="帖子详情">
            {detail && (
              <ActivityCard
                key={detail.ref}
                entry={detail}
                detail
                onImage={(images, index) => setViewing({ images, index })}
                onDelete={remove}
                onPublic={togglePublic}
                onShare={setSharing}
                onDeleteComment={deleteComment}
                onPublicComment={togglePublicComment}
                onPerson={owner ? openPerson : undefined}
              />
            )}
            {!detail && !detailError && <p className="feed-status">加载中…</p>}
            {detailError && (
              <p className="feed-status" role="status">
                {detailError}
              </p>
            )}
          </section>
        </>
      )}

      {notice && (
        <div className="toast toast--notice" role="status">
          {notice}
        </div>
      )}
      {undo && (
        <div className="toast" role="status">
          已删除
          <button type="button" onClick={restore}>
            撤销
          </button>
        </div>
      )}

      {sharing && (
        <Share
          entry={sharing}
          onClose={() => setSharing(null)}
          onChange={(updated) => {
            setSharing(updated)
            replace(updated)
          }}
        />
      )}

      <ImageViewer viewing={viewing} onClose={() => setViewing(null)} onStep={step} />
    </>
  )
}

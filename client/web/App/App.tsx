import './styles.css'
import { useCallback, useEffect, useRef, useState } from 'react'
import Timeline from './Timeline/Timeline'
import Login from './Login'
import Settings from './Settings/Settings'
import PageHeader from './PageHeader'

const POST_PATH = /^\/([A-Za-z0-9][A-Za-z0-9-]{1,38}[A-Za-z0-9])$/
export default function App() {
  const [path, setPath] = useState(() => location.pathname)
  const [owner, setOwner] = useState(false)
  const [title, setTitle] = useState(document.title)
  const [sessionVersion, setSessionVersion] = useState(0)
  const titleClicks = useRef({ count: 0, at: 0 })
  const onLogin = path === '/login'
  const onSettings = path.startsWith('/settings/')
  const active = !onLogin && !onSettings
  const postId = active ? (POST_PATH.exec(path)?.[1] ?? null) : null
  const navigate = useCallback((to: string, replace = false) => {
    if (replace) history.replaceState(history.state, '', to)
    else history.pushState({ atraceBack: true, atraceFrom: location.pathname }, '', to)
    setPath(to)
    window.scrollTo(0, 0)
  }, [])
  function back() {
    if (onSettings && path !== '/settings/' && history.state?.atraceFrom !== '/settings/') {
      navigate('/settings/', true)
      return
    }
    if (history.state?.atraceBack) history.back()
    else navigate('/', true)
  }
  function titleClick(event: React.MouseEvent): boolean {
    if ((event.target as HTMLElement).closest('button,a,form,input,.filter')) return true
    if (!owner && !onLogin) {
      const at = performance.now()
      const last = titleClicks.current
      const count = at - last.at < 500 ? last.count + 1 : 1
      titleClicks.current = { count, at }
      if (count === 3) {
        navigate('/login')
        return true
      }
    }
    if (path !== '/') {
      back()
      return true
    }
    return false
  }
  const markOwner = useCallback(() => setOwner(true), [])
  useEffect(() => {
    let current = true
    void fetch('/api/site')
      .then(async (response) => {
        if (!response.ok || !current) return
        const site: { title: string; owner: boolean } = await response.json()
        if (!current) return
        setTitle(site.title)
        if (site.owner) setOwner(true)
      })
      .catch(() => {})
    return () => {
      current = false
    }
  }, [])
  useEffect(() => {
    document.title = title
  }, [title])
  useEffect(() => {
    const link = document.querySelector<HTMLLinkElement>(
      'link[rel="alternate"][type="text/markdown"]',
    )
    if (link) link.href = active && postId ? `/${postId}.md` : '/index.md'
  }, [active, postId])
  useEffect(() => {
    const pop = () => setPath(location.pathname)
    addEventListener('popstate', pop)
    return () => removeEventListener('popstate', pop)
  }, [])
  return (
    <div className="layout">
      <main>
        <Timeline
          path={path}
          postId={postId}
          active={active}
          title={title}
          owner={owner}
          setOwner={setOwner}
          sessionVersion={sessionVersion}
          onNavigate={navigate}
          onBack={back}
          onTitleClick={titleClick}
        />
        {!active && (
          <>
            {onLogin && (
              <PageHeader title={title} subpage onBack={back} onTitleClick={titleClick}>
                <span className="intro-spacer" aria-hidden="true" />
              </PageHeader>
            )}
            {onLogin ? (
              <Login
                onSuccess={() => {
                  setOwner(true)
                  setSessionVersion((v) => v + 1)
                  navigate('/', true)
                }}
              />
            ) : (
              <Settings
                path={path}
                onNavigate={navigate}
                onBack={back}
                title={title}
                onTitle={setTitle}
                onOwner={markOwner}
                onLogin={() => navigate('/login', true)}
              />
            )}
          </>
        )}
      </main>
    </div>
  )
}

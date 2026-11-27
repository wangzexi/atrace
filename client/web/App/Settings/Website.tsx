import { useEffect, useRef, useState, type FormEvent } from 'react'

export default function Website({
  title,
  onTitle,
  footer,
  onFooter,
  onLogin,
}: {
  title: string
  onTitle: (title: string) => void
  footer: string | undefined
  onFooter: (footer: string) => void
  onLogin: () => void
}) {
  const [draft, setDraft] = useState<string | null>(null)
  const [status, setStatus] = useState('')
  const timer = useRef<number>()
  const latest = useRef({ draft, title })
  latest.current = { draft, title }

  // The title is saved when typing stops, when the field loses focus and when the page is left: there is no save button.
  async function persist() {
    window.clearTimeout(timer.current)
    timer.current = undefined
    const { draft, title } = latest.current
    const next = (draft ?? title).trim()
    if (draft === null || !next || next === title) return
    setStatus('正在保存…')
    try {
      const response = await fetch('/admin/settings', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ title: next }),
      })
      if (!response.ok) {
        if (response.status === 401) {
          onLogin()
          return
        }
        throw new Error(
          response.status === 400 ? '标题需要为 1 到 80 个字符，没有保存。' : '保存失败，请重试。',
        )
      }
      onTitle(next)
      setStatus('已自动保存')
      window.setTimeout(() => setStatus((now) => (now === '已自动保存' ? '' : now)), 2500)
    } catch (error) {
      setStatus((error as Error).message)
    }
  }
  const flush = useRef(persist)
  flush.current = persist
  useEffect(
    () => () => {
      if (timer.current !== undefined) void flush.current()
    },
    [],
  )

  function edit(value: string) {
    setDraft(value)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => void flush.current(), 900)
  }

  return (
    <>
      <div className="settings-section">
        <label className="field">
          <span>网站标题</span>
          <input
            id="site-title"
            placeholder=" "
            value={draft ?? title}
            required
            maxLength={80}
            onChange={(event) => edit(event.target.value)}
            onBlur={() => void persist()}
          />
        </label>
      </div>
      <FooterSettings footer={footer} onSave={onFooter} onLogin={onLogin} />
      {status && (
        <div className="toast toast--notice" role="status">
          {status}
        </div>
      )}
    </>
  )
}

function FooterSettings({
  footer,
  onSave,
  onLogin,
}: {
  footer: string | undefined
  onSave: (footer: string) => void
  onLogin: () => void
}) {
  const [draft, setDraft] = useState<string>()
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)
  const value = draft ?? footer ?? ''
  const disabled = busy || footer === undefined

  async function save(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setMessage('')
    try {
      const response = await fetch('/admin/settings/footer', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ html: value }),
      })
      if (response.status === 401) {
        onLogin()
        return
      }
      if (!response.ok) {
        setMessage(response.status === 400 ? (await response.text()).trim() : '保存失败，请重试。')
        return
      }
      const saved: { html: string } = await response.json()
      onSave(saved.html)
      setDraft(undefined)
      setMessage('页脚已保存')
    } catch {
      setMessage('保存失败，请重试。')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="settings-section">
      <h2>首页页脚</h2>
      <form className="site-footer-fields" onSubmit={(event) => void save(event)}>
        <div className="field">
          <label htmlFor="site-footer-html">页脚 HTML</label>
          <textarea
            id="site-footer-html"
            value={value}
            placeholder="留空则隐藏页脚"
            rows={4}
            maxLength={4096}
            disabled={disabled}
            onChange={(event) => setDraft(event.target.value)}
          />
        </div>
        <button className="account-button" type="submit" disabled={disabled || draft === undefined}>
          保存页脚
        </button>
        {message && (
          <p className="settings-note" role="status">
            {message}
          </p>
        )}
      </form>
    </div>
  )
}

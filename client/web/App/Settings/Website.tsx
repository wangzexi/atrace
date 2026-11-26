import { useEffect, useRef, useState } from 'react'

export default function Website({
  title,
  onTitle,
  onLogin,
}: {
  title: string
  onTitle: (title: string) => void
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
      {status && (
        <div className="toast toast--notice" role="status">
          {status}
        </div>
      )}
    </>
  )
}

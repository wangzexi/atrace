// Changing the owner's password. The browser that changes it stays signed in; the others are signed out.
import { useState, type FormEvent } from 'react'

export default function Password() {
  const [current, setCurrent] = useState('')
  const [next, setNext] = useState('')
  const [again, setAgain] = useState('')
  const [message, setMessage] = useState('')
  const [busy, setBusy] = useState(false)

  async function change(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (next !== again) {
      setMessage('两次输入的新密码不一样。')
      return
    }
    setBusy(true)
    setMessage('')
    try {
      const response = await fetch('/admin/password', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ current, password: next }),
      })
      if (!response.ok) {
        const text = (await response.text()).trim()
        setMessage(
          response.status === 401
            ? '当前密码不对，或者登录已过期。'
            : response.status === 400 || response.status === 429
              ? text
              : '修改失败，请重试。',
        )
        return
      }
      setCurrent('')
      setNext('')
      setAgain('')
      setMessage('密码已修改。其他浏览器需要用新密码重新登录。')
    } catch {
      setMessage('修改失败，请重试。')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="settings-section settings-password">
      <form
        id="password-fields"
        className="password-fields"
        onSubmit={(event) => void change(event)}
      >
        <label className="field">
          <span>当前密码</span>
          <input
            placeholder=" "
            type="password"
            autoComplete="current-password"
            value={current}
            required
            onChange={(event) => setCurrent(event.target.value)}
          />
        </label>
        <label className="field">
          <span>新密码（至少 8 个字符）</span>
          <input
            placeholder=" "
            type="password"
            autoComplete="new-password"
            value={next}
            required
            minLength={8}
            onChange={(event) => setNext(event.target.value)}
          />
        </label>
        <label className="field">
          <span>再输入一次新密码</span>
          <input
            placeholder=" "
            type="password"
            autoComplete="new-password"
            value={again}
            required
            minLength={8}
            onChange={(event) => setAgain(event.target.value)}
          />
        </label>
        <button className="account-button" type="submit" disabled={busy}>
          确认修改
        </button>
      </form>
      {message && (
        <p className="settings-note" role="status">
          {message}
        </p>
      )}
    </div>
  )
}

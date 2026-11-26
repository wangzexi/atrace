import { useState, type FormEvent } from 'react'

export default function Login({ onSuccess }: { onSuccess: () => void }) {
  const [pending, setPending] = useState(false)
  const [error, setError] = useState('')

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const body = new URLSearchParams()
    const password = new FormData(event.currentTarget).get('password')
    body.set('password', typeof password === 'string' ? password : '')
    setError('')
    setPending(true)
    try {
      const response = await fetch('/login', { method: 'POST', body })
      if (!response.ok) {
        setError((await response.text()).trim())
        return
      }
      onSuccess()
    } catch {
      setError('登录失败，请重试。')
    } finally {
      setPending(false)
    }
  }

  return (
    <section className="account-view login" aria-labelledby="login-title">
      <h2 id="login-title">登录</h2>
      <form method="post" action="/login" onSubmit={submit}>
        <div className="account-field">
          <label htmlFor="password">密码</label>
          <input
            id="password"
            name="password"
            type="password"
            autoComplete="current-password"
            required
          />
        </div>
        {error && <p role="alert">{error}</p>}
        <button className="account-button account-button--primary" type="submit" disabled={pending}>
          {pending ? '登录中…' : '登录'}
        </button>
      </form>
    </section>
  )
}

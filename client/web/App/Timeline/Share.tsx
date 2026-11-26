import { useEffect, useRef, useState } from 'react'
import { Check, Copy, X } from 'lucide-react'
import { RULES, setAlias, type Entry } from '../api'

const ALIAS = /^[a-z0-9][a-z0-9-]{1,38}[a-z0-9]$/

// The share-link dialog of an entry. The link is /<ref> until the owner types a readable alias,
// which takes its place; the alias saves as it is typed.
export default function Share({
  entry,
  onClose,
  onChange,
}: {
  entry: Entry
  onClose: () => void
  onChange: (entry: Entry) => void
}) {
  const dialog = useRef<HTMLDialogElement>(null)
  const [alias, setAliasText] = useState(entry.alias)
  const [error, setError] = useState('')
  const [copied, setCopied] = useState(false)

  useEffect(() => dialog.current?.showModal(), [])

  // Saves the alias; true when the entry has it afterwards.
  async function commit(value: string): Promise<boolean> {
    if (value === entry.alias) return true
    if (value && !ALIAS.test(value)) {
      setError(RULES)
      return false
    }
    try {
      await setAlias(entry.ref, value)
      setError('')
      onChange({ ...entry, alias: value })
      return true
    } catch (e) {
      setError((e as Error).message)
      return false
    }
  }

  // The alias is saved a moment after the typing stops.
  const latest = useRef(commit)
  latest.current = commit
  useEffect(() => {
    const timer = window.setTimeout(() => void latest.current(alias.trim()), 600)
    return () => window.clearTimeout(timer)
  }, [alias])

  async function copy() {
    const value = alias.trim()
    if (!(await commit(value))) return
    try {
      await navigator.clipboard.writeText(`${location.origin}/${value || entry.ref}`)
      setCopied(true)
      window.setTimeout(() => setCopied(false), 1500)
    } catch {
      setError('复制失败，请手动复制地址栏里的链接。')
    }
  }

  return (
    <dialog
      ref={dialog}
      className="share"
      aria-label="分享"
      onClose={onClose}
      onClick={(event) => event.target === dialog.current && dialog.current?.close()}
    >
      <div className="share-head">
        <h2>分享</h2>
        <button
          className="share-close"
          type="button"
          aria-label="关闭"
          onClick={() => dialog.current?.close()}
        >
          <X aria-hidden="true" />
        </button>
      </div>
      <div className="share-alias-row">
        <span>{location.origin}/</span>
        <input
          aria-label="自定义链接"
          readOnly={entry.activityId <= 0}
          value={alias}
          maxLength={40}
          placeholder={entry.ref}
          autoCapitalize="none"
          spellCheck={false}
          onChange={(event) => setAliasText(event.target.value.toLowerCase())}
        />
      </div>
      {error && (
        <p className="share-error" role="alert">
          {error}
        </p>
      )}
      <button type="button" className="share-copy" onClick={copy}>
        {copied ? <Check aria-hidden="true" /> : <Copy aria-hidden="true" />}
        {copied ? '已复制' : '复制链接'}
      </button>
    </dialog>
  )
}

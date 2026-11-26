import { ChevronRight, GripVertical } from 'lucide-react'
// The people the owner has told apart: a name and the accounts, on any platform, that are theirs. One of them is the
// owner: the timeline opens on what their accounts did. The accounts the devices report that nobody has yet are
// offered here, to give to whoever they belong to.
import { useCallback, useEffect, useRef, useState } from 'react'
import {
  createPerson,
  deletePerson,
  fetchPeople,
  orderPeople,
  Unauthorized,
  updatePerson,
  type People as PeopleData,
  type Person,
} from '../api'
import { subscribe } from '../events'
import { mergedAccounts, platformOf } from './merge'

export default function People({ onLogin, onOwner }: { onLogin: () => void; onOwner: () => void }) {
  const [data, setData] = useState<PeopleData | null>(null)
  const [notice, setNotice] = useState('')
  const [adding, setAdding] = useState(false)
  const [newName, setNewName] = useState('')
  const list = useRef<HTMLUListElement>(null)
  const [drag, setDrag] = useState<{
    id: number
    start: number
    y: number
    slot: number
    active: boolean
  } | null>(null)
  const [saving, setSaving] = useState(false)

  async function move(id: number, slot: number) {
    if (!data || saving) return
    const people = [...data.people]
    const from = people.findIndex((person) => person.id === id)
    if (from < 0 || from === slot) return
    const [person] = people.splice(from, 1)
    people.splice(slot, 0, person)
    setData({ ...data, people })
    setSaving(true)
    try {
      await change(
        () => orderPeople(people.map((item) => item.id)),
        people[0].id !== data.people[0].id,
      )
    } finally {
      setSaving(false)
    }
  }

  const load = useCallback(async () => {
    try {
      setData(await fetchPeople())
    } catch (error) {
      if (error instanceof Unauthorized) onLogin()
      else setNotice('无法加载人物。')
    }
  }, [onLogin])
  useEffect(() => {
    void load()
  }, [load])
  // The accounts on offer are what the devices report, which changes as they do.
  useEffect(() => subscribe('collectors', () => void load(), 500, 3000), [load])

  // Each change goes to the server and the list is read again, so what is shown is what is kept.
  async function change(work: () => Promise<void>, ownerChanged = false) {
    setNotice('')
    try {
      await work()
      if (ownerChanged) onOwner()
    } catch (error) {
      if (error instanceof Unauthorized) {
        onLogin()
        return
      }
      setNotice((error as Error).message)
    }
    await load()
  }

  if (!data)
    return (
      <div className="settings-section">
        <p className="settings-note">{notice || '正在加载人物…'}</p>
      </div>
    )
  const { people, offered } = data
  const accountIds = (person: Person) => person.accounts.map((account) => account.id)
  return (
    <div className="settings-section settings-section--list">
      <p className="settings-note">将同一个人的平台账号归到一起。</p>
      {offered.length > 0 && (
        <p className="settings-note" aria-label="还没有归属的账号">
          还没有归属的账号：
          {offered
            .map((account) =>
              account.name ? `${platformOf(account.id)} · ${account.name}` : account.id,
            )
            .join('，')}
        </p>
      )}
      <ul className="people" ref={list} aria-label="人物排序" aria-busy={saving}>
        {people.map((person, index) => {
          const source = drag ? people.findIndex((item) => item.id === drag.id) : -1
          const boundary = drag ? drag.slot + (drag.slot > source ? 1 : 0) : -1
          return (
            <li
              key={person.id}
              data-person={person.id}
              className={
                'person' +
                (drag?.active && drag.id === person.id ? ' person--dragging' : '') +
                (drag?.active && index === boundary && drag.slot !== source
                  ? ' person--drop-before'
                  : '') +
                (drag?.active && boundary === people.length && index === people.length - 1
                  ? ' person--drop-after'
                  : '')
              }
              style={
                drag?.active && drag.id === person.id
                  ? { transform: `translateY(${drag.y - drag.start}px)` }
                  : undefined
              }
            >
              {people.length > 1 && (
                <button
                  className="person-drag"
                  type="button"
                  disabled={saving}
                  aria-label={`调整${person.name}的顺序`}
                  title="拖动排序；方向键上移或下移，第一位为默认人物"
                  onPointerDown={(event) => {
                    if (event.button !== 0) return
                    event.preventDefault()
                    event.currentTarget.focus()
                    event.currentTarget.setPointerCapture(event.pointerId)
                    setDrag({
                      id: person.id,
                      start: event.clientY,
                      y: event.clientY,
                      slot: index,
                      active: false,
                    })
                  }}
                  onPointerMove={(event) => {
                    if (!drag || drag.id !== person.id) return
                    const rows = [...(list.current?.children ?? [])] as HTMLElement[]
                    const others = rows.filter((row) => Number(row.dataset.person) !== person.id)
                    const slot = others.filter((row) => {
                      const bounds = row.getBoundingClientRect()
                      return event.clientY > bounds.top + bounds.height / 2
                    }).length
                    setDrag({
                      ...drag,
                      y: event.clientY,
                      slot,
                      active: drag.active || Math.abs(event.clientY - drag.start) > 5,
                    })
                    if (event.clientY < 60) window.scrollBy(0, -12)
                    else if (event.clientY > window.innerHeight - 60) window.scrollBy(0, 12)
                  }}
                  onPointerUp={() => {
                    if (drag?.active) void move(person.id, drag.slot)
                    setDrag(null)
                  }}
                  onPointerCancel={() => setDrag(null)}
                  onLostPointerCapture={() => setDrag(null)}
                  onKeyDown={(event) => {
                    if (event.key === 'Escape') setDrag(null)
                    if (event.key !== 'ArrowUp' && event.key !== 'ArrowDown') return
                    event.preventDefault()
                    void move(
                      person.id,
                      Math.max(
                        0,
                        Math.min(people.length - 1, index + (event.key === 'ArrowUp' ? -1 : 1)),
                      ),
                    )
                  }}
                >
                  <GripVertical size={18} aria-hidden="true" />
                </button>
              )}
              <details className="person-details">
                <summary className="person-summary">
                  <span>
                    <strong>{person.name}</strong>
                    <small>
                      {[...new Set(person.accounts.map((account) => platformOf(account.id)))].join(
                        '、',
                      ) || '还没有账号'}
                    </small>
                  </span>
                  <ChevronRight size={16} aria-hidden="true" />
                </summary>
                <div className="person-expanded">
                  <div className="person-head">
                    <input
                      className="person-name"
                      aria-label="改名"
                      defaultValue={person.name}
                      maxLength={80}
                      onBlur={(event) => {
                        const name = event.target.value.trim()
                        if (name && name !== person.name)
                          void change(
                            () => updatePerson(person.id, name, accountIds(person)),
                            person.owner,
                          )
                        else event.target.value = person.name
                      }}
                    />
                  </div>
                  <ul className="person-accounts" aria-label="账号">
                    {person.accounts.map((account) => (
                      <li key={account.id} title={account.id}>
                        <span className="person-platform">{platformOf(account.id)}</span>
                        <span className="person-account-name">{account.name || account.id}</span>
                        <button
                          className="chip-remove"
                          type="button"
                          aria-label={`移除 ${account.id}`}
                          onClick={() =>
                            void change(
                              () =>
                                updatePerson(
                                  person.id,
                                  person.name,
                                  accountIds(person).filter((id) => id !== account.id),
                                ),
                              person.owner,
                            )
                          }
                        >
                          ×
                        </button>
                      </li>
                    ))}
                    {person.accounts.length === 0 && <li>还没有账号</li>}
                  </ul>
                  <div className="person-actions">
                    {offered.length > 0 && (
                      <label>
                        <span className="pref-hint">关联账号</span>{' '}
                        <select
                          value=""
                          onChange={(event) => {
                            const id = event.target.value
                            if (id)
                              void change(
                                () =>
                                  updatePerson(person.id, person.name, [...accountIds(person), id]),
                                person.owner,
                              )
                          }}
                        >
                          <option value="">选择…</option>
                          {offered.map((account) => (
                            <option key={account.id} value={account.id}>
                              {account.name
                                ? `${platformOf(account.id)} · ${account.name}`
                                : account.id}
                            </option>
                          ))}
                        </select>
                      </label>
                    )}
                    {people.length > 1 && !person.owner && (
                      <label>
                        <span className="pref-hint">把这个人合并到</span>{' '}
                        <select
                          value=""
                          onChange={(event) => {
                            const target = people.find(
                              (other) => other.id === Number(event.target.value),
                            )
                            if (
                              !target ||
                              !window.confirm(`把「${person.name}」的账号并入「${target.name}」？`)
                            )
                              return
                            void change(async () => {
                              await updatePerson(
                                target.id,
                                target.name,
                                mergedAccounts(target, person),
                              )
                              await deletePerson(person.id)
                            }, target.owner)
                          }}
                        >
                          <option value="">选择…</option>
                          {people
                            .filter((other) => other.id !== person.id)
                            .map((other) => (
                              <option key={other.id} value={other.id}>
                                {other.name}
                              </option>
                            ))}
                        </select>
                      </label>
                    )}
                    {!person.owner && (
                      <button
                        className="account-button"
                        type="button"
                        onClick={() => {
                          if (window.confirm(`删除「${person.name}」？他的账号和内容仍然保留。`))
                            void change(() => deletePerson(person.id))
                        }}
                      >
                        删除
                      </button>
                    )}
                  </div>
                </div>
              </details>
            </li>
          )
        })}
      </ul>
      {adding ? (
        <form
          className="person-new"
          onSubmit={(event) => {
            event.preventDefault()
            const name = newName.trim()
            if (name)
              void change(async () => {
                await createPerson(name, [])
                setNewName('')
                setAdding(false)
              })
          }}
        >
          <label className="field">
            <span>新增人物</span>
            <input
              autoFocus
              placeholder=" "
              maxLength={80}
              value={newName}
              onChange={(event) => setNewName(event.target.value)}
            />
          </label>
          <button className="account-button" type="submit" disabled={!newName.trim()}>
            新建
          </button>
          <button
            className="account-button"
            type="button"
            onClick={() => {
              setNewName('')
              setAdding(false)
            }}
          >
            取消
          </button>
        </form>
      ) : (
        <button
          className="account-button person-new-toggle"
          type="button"
          onClick={() => setAdding(true)}
        >
          新增人物
        </button>
      )}
      {notice && (
        <div className="toast toast--notice" role="status">
          {notice}
        </div>
      )}
    </div>
  )
}

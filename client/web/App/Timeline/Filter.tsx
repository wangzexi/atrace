import { useEffect, useRef, useState } from 'react'
import { Regex, Search } from 'lucide-react'
import { fetchPeople, KINDS, type Person } from '../api'
import { type Choice, type useTimeline } from './useTimeline'

type FilterState = Pick<
  ReturnType<typeof useTimeline>,
  | 'owner'
  | 'query'
  | 'setQuery'
  | 'regex'
  | 'setRegex'
  | 'choice'
  | 'choose'
  | 'person'
  | 'viewPerson'
  | 'filtered'
  | 'visibility'
  | 'chooseVisibility'
>
export default function Filter({ timeline }: { timeline: FilterState }) {
  const {
    owner,
    query,
    setQuery,
    regex,
    setRegex,
    choice,
    choose,
    person,
    viewPerson,
    filtered,
    visibility,
    chooseVisibility,
  } = timeline
  const filterRef = useRef<HTMLDivElement>(null)
  const [panelOpen, setPanelOpen] = useState(false)
  const [filterPeople, setFilterPeople] = useState<Person[]>([])
  useEffect(() => {
    if (!owner || !panelOpen) return
    let active = true
    void fetchPeople()
      .then(({ people }) => {
        if (active) setFilterPeople(people)
      })
      .catch(() => {
        if (active) setFilterPeople([])
      })
    return () => {
      active = false
    }
  }, [owner, panelOpen])

  // The filter panel closes on a click elsewhere or on Esc.
  useEffect(() => {
    if (!panelOpen) return
    const away = (event: MouseEvent) => {
      if (!filterRef.current?.contains(event.target as Node)) setPanelOpen(false)
    }
    const escape = (event: KeyboardEvent) => event.key === 'Escape' && setPanelOpen(false)
    document.addEventListener('mousedown', away)
    document.addEventListener('keydown', escape)
    return () => {
      document.removeEventListener('mousedown', away)
      document.removeEventListener('keydown', escape)
    }
  }, [panelOpen])

  return (
    <div className="filter" ref={filterRef} onClick={(event) => event.stopPropagation()}>
      <button
        className="filter-button"
        type="button"
        aria-label="筛选和搜索"
        aria-expanded={panelOpen}
        data-tip={panelOpen ? undefined : '搜索'}
        data-active={filtered}
        onClick={() => setPanelOpen(!panelOpen)}
      >
        <Search aria-hidden="true" strokeWidth={2} />
      </button>
      {panelOpen && (
        <div className="filter-panel" role="dialog" aria-label="筛选和搜索">
          <div className="side-search" role="search">
            <Search aria-hidden="true" />
            <input
              autoFocus
              type="search"
              value={query}
              placeholder="搜索"
              maxLength={512}
              aria-label="搜索历史笔记"
              onChange={(event) => setQuery(event.target.value)}
              onKeyDown={(event) =>
                event.key === 'Escape' && query && (event.stopPropagation(), setQuery(''))
              }
            />
            <label className="search-regex" data-tip="正则表达式">
              <input
                type="checkbox"
                checked={regex}
                onChange={(event) => setRegex(event.target.checked)}
              />
              <Regex aria-hidden="true" />
            </label>
          </div>
          <div className="side-types" role="group" aria-label="类型">
            {(
              [{ kind: 'all', label: '全部' }, ...KINDS] as {
                kind: Choice
                label: string
              }[]
            ).map((item) => (
              <button
                key={item.kind}
                type="button"
                className="chip"
                aria-pressed={choice === item.kind}
                onClick={() => choose(item.kind)}
              >
                {item.label}
              </button>
            ))}
          </div>
          {owner && (
            <div className="side-types" role="group" aria-label="公开状态">
              {(
                [
                  ['all', '全部'],
                  ['public', '已公开'],
                  ['private', '未公开'],
                ] as const
              ).map(([value, label]) => (
                <button
                  key={value}
                  type="button"
                  className="chip"
                  aria-pressed={visibility === value}
                  onClick={() => chooseVisibility(value)}
                >
                  {label}
                </button>
              ))}
            </div>
          )}
          {owner && filterPeople.length > 0 && (
            <div className="side-types" role="group" aria-label="人物">
              {filterPeople.map((item, index) => {
                const isDefault = index === 0 || item.owner
                const id = isDefault ? 0 : item.id
                return (
                  <button
                    key={item.id}
                    type="button"
                    className="chip"
                    aria-pressed={person.id === id}
                    onClick={() => {
                      if (person.id !== id) viewPerson({ id, name: isDefault ? '' : item.name })
                    }}
                  >
                    {item.name}
                  </button>
                )
              })}
            </div>
          )}
        </div>
      )}
    </div>
  )
}

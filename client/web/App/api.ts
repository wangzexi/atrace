// The server API fields and requests used by the application.
export type Visibility = 'all' | 'public' | 'private'

export interface Reply {
  author: string
  // The owner gets the account behind a name, to open the person it belongs to.
  authorId?: string
  published: string
  content: string
  // For the owner: the comment's name, and whether a re-check found it deleted on its platform.
  id?: string
  gone?: boolean
  // Named by its ref for the owner's edits. The owner's own reply also has its entry, and whether it is public.
  ref?: string
  activityId?: number
  public?: boolean
  // Who a reply inside a thread answers; absent for a comment on the post itself.
  replyTo?: string
}

// A post, its text as it was written: plain text, or Markdown when markdown is set.
export interface Post {
  id: string
  author: string
  authorId?: string
  title: string
  content: string
  markdown: boolean
  published: string
  url: string
  // The post this one answers, by ID; set even when that post was never collected.
  inReplyTo?: string
  images: string[] | null
  videos: string[] | null
  livePhotos?: Record<string, string>
  // The archive's copy of each picture it holds, by the picture's original address.
  attachments?: Record<string, string>
}

export interface Entry {
  activityId: number
  ref: string
  gone: boolean
  dropped: boolean
  public: boolean
  alias: string
  // The post the entry is about; for a repost, the repost's own ID.
  id: string
  type: 'Create' | 'Announce' | 'Like' | 'Add'
  actor: string
  actorId?: string
  url: string
  published: string
  observed: string
  // The owner's own words, when the entry has any; quote is another post shown as a card; replyTo is the post answered.
  post: Post | null
  quote: Post | null
  replyTo: Post | null
  replies: Reply[] | null
}

export interface Page {
  entries: Entry[]
  next: string
  more: boolean
  owner: boolean
}

export type Kind = 'Create' | 'Reply' | 'Announce' | 'Like' | 'Add'

export const KINDS: { kind: Kind; label: string }[] = [
  { kind: 'Create', label: '原创' },
  { kind: 'Reply', label: '回复' },
  { kind: 'Announce', label: '转发' },
  { kind: 'Like', label: '点赞' },
  { kind: 'Add', label: '收藏' },
]

// What a failed page of the timeline says. It is not shown: the list stops asking for more until the button is pressed,
// and the button is the way to try again.
export const LOAD_FAILED = 'load failed'

// One page of the timeline. The server does the searching, page by page, over everything stored:
// text is looked for in the entries' own words, their post, its author, the post they answer and
// the comments; regex reads it as a regular expression. person is whose timeline it is, 0 for the owner's own.
export async function fetchEntries(
  after: string,
  kinds: Kind[],
  text = '',
  regex = false,
  person = 0,
  visibility: Visibility = 'all',
  signal?: AbortSignal,
): Promise<Page> {
  const query =
    (text ? `&q=${encodeURIComponent(text)}${regex ? '&regex=1' : ''}` : '') +
    (person ? `&person=${person}` : '')
  const response = await fetch(
    `/api/entries?after=${encodeURIComponent(after)}&types=${kinds.join(',')}&visibility=${visibility}${query}`,
    {
      signal,
    },
  )
  if (response.status === 400 && regex) throw new Error('正则表达式不正确。')
  if (!response.ok) throw new Error(LOAD_FAILED)
  return response.json()
}

// A person is the owner's note on who is who: a name and the accounts, on any platform, that are theirs.
export interface Person {
  id: number
  name: string
  owner: boolean
  accounts: { id: string; name: string }[]
}

async function ownerFetch(path: string, init?: RequestInit): Promise<Response> {
  const response = await fetch(path, init)
  if (response.status === 401) throw new Unauthorized()
  if (!response.ok)
    throw new Error(response.status === 400 ? '这个改动不被接受。' : '操作失败，请重试。')
  return response
}

const json = (method: string, body: unknown): RequestInit => ({
  method,
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify(body),
})

// The people, the owner first, and the accounts the devices report that belong to nobody yet.
export interface People {
  people: Person[]
  offered: { id: string; name: string }[]
}

export async function fetchPeople(): Promise<People> {
  return (await ownerFetch('/admin/people')).json()
}

export async function createPerson(name: string, accounts: string[]): Promise<number> {
  return (await (await ownerFetch('/admin/people', json('POST', { name, accounts }))).json()).id
}

export async function updatePerson(id: number, name: string, accounts: string[]): Promise<void> {
  await ownerFetch(`/admin/people/${id}`, json('PUT', { name, accounts }))
}

export async function deletePerson(id: number): Promise<void> {
  await ownerFetch(`/admin/people/${id}`, { method: 'DELETE' })
}

// The person an account belongs to, 0 when it belongs to none.
export async function personOfAccount(account: string): Promise<number> {
  return (
    await (await ownerFetch(`/admin/people/of?account=${encodeURIComponent(account)}`)).json()
  ).id
}

// What a device says of itself: the collectors it has and how each is doing. The server shows it and reads none of it.
export interface Device {
  device: string
  online: boolean
  reportedAt: string
  collectors: {
    name: string
    account?: string
    state?: string
    lastRun?: string
    nextRun?: string
    message?: string
  }[]
}

export async function fetchDevices(): Promise<Device[]> {
  return (await (await ownerFetch('/admin/devices')).json()).devices
}

// One entry's or comment's edits, named by its ref: only the fields given are changed.
function editEntry(
  ref: string,
  fields: { hidden?: boolean; public?: boolean; alias?: string },
): Promise<Response> {
  return fetch(`/admin/entry/${ref}`, json('PATCH', fields))
}

// Deleting is soft: the entry, or a comment the platform says is deleted, is hidden from every reader and can be restored.
export async function setHidden(ref: string, hidden: boolean): Promise<void> {
  if (!(await editEntry(ref, { hidden })).ok) throw new Error('操作失败，请重试。')
}

// Entries are private until made public.
export async function setPublic(ref: string, value: boolean): Promise<void> {
  if (!(await editEntry(ref, { public: value })).ok) throw new Error('操作失败，请重试。')
}

// A readable address for the entry, /<alias>; an empty alias takes it away.
export async function setAlias(ref: string, alias: string): Promise<void> {
  const response = await editEntry(ref, { alias })
  if (response.ok) return
  const body = await response.json().catch(() => null)
  throw new Error(
    response.status === 400 && body?.error?.message
      ? aliasMessage(body.error.message)
      : '操作失败，请重试。',
  )
}

export const RULES =
  '链接要 3 到 40 个小写字母、数字或连字符，不能以连字符开头或结尾，也不能是 8 位字母数字或网站自己用的名字。'

function aliasMessage(message: string): string {
  return message.includes('already used') ? '这个链接已被别的条目用了。' : RULES
}

export async function readEntry(ref: string, signal?: AbortSignal): Promise<Entry> {
  const response = await fetch(`/api/entry?${new URLSearchParams({ ref })}`, { signal })
  if (!response.ok) throw new Error('加载失败。')
  return response.json()
}

// The owner opened the entry: a device is asked to read its post again, comments included. What it finds arrives
// through the entries signal like any other change; nothing here waits for it, and a failure changes nothing.
export function wantPost(ref: string): void {
  void fetch('/admin/fetch', json('POST', { ref })).catch(() => {})
}

// Thrown when the viewer is not the owner.
export class Unauthorized extends Error {}

export async function orderPeople(people: number[]): Promise<void> {
  await ownerFetch('/admin/people/order', json('PUT', { people }))
}

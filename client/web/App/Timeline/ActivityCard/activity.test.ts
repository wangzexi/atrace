import { describe, expect, it } from 'vitest'
import type { Entry, Post } from '../../api'
import { galleryOf, timeOf, titleOf, verbOf } from './activity'
import { formatDate, formatWhen } from './time'

const post = (extra: Partial<Post> = {}): Post => ({
  id: 'x:post:1',
  author: 'a',
  title: '',
  content: 'text',
  markdown: false,
  published: '',
  url: '',
  images: null,
  videos: null,
  ...extra,
})
const entry = (extra: Partial<Entry>): Entry => ({
  activityId: 1,
  ref: 'r',
  gone: false,
  dropped: false,
  public: false,
  alias: '',
  id: 'x:post:1',
  type: 'Create',
  actor: 'me',
  url: '',
  published: '',
  observed: '2026-11-01T00:00:00Z',
  post: null,
  quote: null,
  replyTo: null,
  replies: null,
  ...extra,
})

describe('the verb of an entry', () => {
  it('names a repost, a like and a favorite', () => {
    expect(verbOf(entry({ type: 'Announce' }))).toBe('转发')
    expect(verbOf(entry({ type: 'Like' }))).toBe('点赞')
    expect(verbOf(entry({ type: 'Add' }))).toBe('收藏')
  })
  it('says nothing for an own post, even one that quotes another', () => {
    expect(verbOf(entry({ post: post(), quote: post({ id: 'x:post:2' }) }))).toBe('')
  })
  it('says 回复 for a post that answers another, collected or not', () => {
    expect(verbOf(entry({ post: post({ inReplyTo: 'x:post:9' }) }))).toBe('回复')
  })
})

describe('the time of an entry', () => {
  it("is the activity's, else the post's, else when it was collected", () => {
    expect(timeOf(entry({ published: 'P', post: post({ published: 'Q' }) }))).toBe('P')
    expect(timeOf(entry({ post: post({ published: 'Q' }) }))).toBe('Q')
    expect(timeOf(entry({ post: post() }))).toBe('2026-11-01T00:00:00Z')
  })
  it("is none for a like or a favorite of someone else's post: the card says when it was written", () => {
    expect(timeOf(entry({ type: 'Like', quote: post({ published: 'Q' }) }))).toBe('')
    expect(timeOf(entry({ type: 'Add', published: 'P', quote: post() }))).toBe('')
  })
  it("is none for a kept note of the owner's words when nothing says when it was kept", () => {
    expect(timeOf(entry({ type: 'Add', post: post(), quote: post({ id: 'z:answer:5' }) }))).toBe('')
    expect(timeOf(entry({ type: 'Add', post: post({ published: 'Q' }), quote: post() }))).toBe('Q')
  })
})

it('has a title only for an own post', () => {
  expect(titleOf(entry({ post: post({ title: 'T' }) }))).toBe('T')
  expect(titleOf(entry({ type: 'Add', post: post({ title: 'T' }) }))).toBe('')
})

describe('times', () => {
  it("formats in the viewer's zone, with the year only when it is not this year", () => {
    const now = new Date()
    const same = new Date(now.getFullYear(), 0, 2, 3, 4).toISOString()
    expect(formatWhen(same)).toBe('1月2日 03:04')
    expect(formatWhen(new Date(2020, 0, 1, 15, 30).toISOString())).toBe('2020年1月1日 15:30')
    expect(formatDate(new Date(2020, 0, 1).toISOString())).toBe('2020/1/1')
  })
  it('shows none for none', () => {
    expect(formatWhen('')).toBe('')
    expect(formatDate('not a time')).toBe('')
  })
})

it('keeps Live Photo motion in its image slot rather than repeating it as a standalone video', () => {
  const gallery = galleryOf(
    post({
      images: ['/still.jpg'],
      videos: ['/motion.mov', '/ordinary.mp4'],
      livePhotos: { '/still.jpg': '/motion.mov' },
    }),
  )
  expect(gallery.images).toEqual(['/still.jpg'])
  expect(gallery.videos).toEqual(['/ordinary.mp4'])
  expect(gallery.livePhotos).toEqual({ '/still.jpg': '/motion.mov' })
})

it('keeps a motion clip accessible when its still image is absent from the gallery', () => {
  expect(
    galleryOf(post({ videos: ['/motion.mov'], livePhotos: { '/still.jpg': '/motion.mov' } }))
      .videos,
  ).toEqual(['/motion.mov'])
})

// The default renderer's rules for an entry: what its header says, which time it shows, what is
// hidden. They follow from the entry's standard fields; a platform's renderer may draw it differently.
import type { Entry, Post } from '../../api'
import { renderMarkdown } from './Content/markdown'

const VERBS: Record<string, string> = { Announce: '转发', Like: '点赞', Add: '收藏' }

// What the owner did, said after their name: a repost, a like, a favorite; for a post that answers another, 回复.
export function verbOf(entry: Entry): string {
  return VERBS[entry.type] ?? (entry.post?.inReplyTo ? '回复' : '')
}

// The time of an entry, ISO or none. A like or a favorite has no time of its own on most platforms, and
// its post is not the owner's text: the card says when that post was written, the header says nothing.
// A kept note of the owner's own words is dated only when something says when it was kept.
export function timeOf(entry: Entry): string {
  const reaction = entry.type === 'Like' || entry.type === 'Add'
  if (reaction && !entry.post) return ''
  return entry.published || entry.post?.published || (reaction ? '' : entry.observed)
}

// An article or answer has a title of its own.
export function titleOf(entry: Entry): string {
  return entry.type === 'Create' ? (entry.post?.title ?? '') : ''
}

// Pictures and videos of a post; those a Markdown text shows in place are not shown twice.
export function galleryOf(post: Post): {
  images: string[]
  videos: string[]
  livePhotos: Record<string, string>
} {
  const inlined = post.markdown ? renderMarkdown(post).inlined : new Set<string>()
  const images = (post.images ?? []).filter((src) => !inlined.has(src))
  const pairedVideos = new Set(images.map((src) => post.livePhotos?.[src]).filter(Boolean))
  return {
    images,
    videos: (post.videos ?? []).filter((src) => !pairedVideos.has(src)),
    livePhotos: post.livePhotos ?? {},
  }
}

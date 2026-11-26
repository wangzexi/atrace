// The verb icon after the owner's name in an entry header.
import { Bookmark, Heart, Repeat2, Reply, type LucideIcon } from 'lucide-react'

// What the owner did with a post, said in the header after their name, with its own colour: a
// filled heart for a like, a bookmark for a favorite, the repost arrows for a repost.
const VERBS: Record<string, { Icon: LucideIcon; name: string; filled: boolean }> = {
  点赞: { Icon: Heart, name: 'like', filled: true },
  收藏: { Icon: Bookmark, name: 'bookmark', filled: true },
  转发: { Icon: Repeat2, name: 'repost', filled: false },
  回复: { Icon: Reply, name: 'reply', filled: false },
}

export default function Verb({ action }: { action: string }) {
  const verb = VERBS[action]
  if (!verb) return null
  return (
    <span className={`entry-verb entry-verb--${verb.name}`}>
      <verb.Icon aria-hidden="true" fill={verb.filled ? 'currentColor' : 'none'} />
      {action}
    </span>
  )
}

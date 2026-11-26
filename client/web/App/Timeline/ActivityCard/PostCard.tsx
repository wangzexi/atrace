// Someone else's post shown as a card inside an entry.
import type { Post } from '../../api'
import Content from './Content/Content'
import Media from './Media'
import Name from './Name'
import { galleryOf } from './activity'
import { formatWhen } from './time'

// Someone else's post, as a card that opens the original (as on X). The card is not a link
// element, because it holds pictures that open the viewer. As a reply's parent it sits above
// the reply. Its text is cut short, on the post's own page too. A reply's parent has 显示更多; a quoted
// card has none, as a click on the card opens the other post, where the rest is.
export default function PostCard({
  post,
  onImage,
  onPerson,
  reply,
}: {
  post: Post
  onImage: (images: string[], index: number) => void
  onPerson?: (account: string, name: string) => void
  reply?: boolean
}) {
  const when = formatWhen(post.published)
  const gallery = galleryOf(post)
  return (
    <div
      className={
        'entry-quote' +
        (post.url ? ' entry-quote--link' : '') +
        (reply ? ' entry-quote--parent' : '')
      }
      onClick={(event) => {
        if (!post.url || window.getSelection()?.toString()) return
        if ((event.target as HTMLElement).closest('a, button, video')) return
        event.stopPropagation()
        window.open(post.url, '_blank', 'noopener')
      }}
    >
      {(post.author || when) && (
        <p className="entry-quote-head">
          <Name
            className="entry-quote-author"
            name={post.author}
            account={post.authorId}
            onPerson={onPerson}
          />
          {when && <span className="entry-quote-when">{when}</span>}
        </p>
      )}
      <div className={reply ? 'entry-parent-line' : undefined}>
        {post.title && (
          <p className={'entry-title' + (post.content ? '' : ' entry-title--only')}>{post.title}</p>
        )}
        <Content post={post} clamp more={!!reply} />
        <Media
          images={gallery.images}
          videos={gallery.videos}
          livePhotos={gallery.livePhotos}
          onImage={onImage}
        />
      </div>
    </div>
  )
}

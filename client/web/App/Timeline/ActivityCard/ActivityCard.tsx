// One entry of the timeline or of a post's own page.
import { formatWhen } from './time'
import { CloudExclamationPoint, Globe, GlobeOff, Share, Trash2 } from 'lucide-react'
import type { Entry as EntryData, Reply as ReplyData } from '../../api'
import Content from './Content/Content'
import Media from './Media'
import Name from './Name'
import PostCard from './PostCard'
import { galleryOf, timeOf, titleOf, verbOf } from './activity'
import ReplyThread from './ReplyThread'
import Verb from './Verb'

export interface Actions {
  onImage: (images: string[], index: number) => void
  onDelete: (entry: EntryData) => void
  onPublic: (entry: EntryData) => void
  onShare: (entry: EntryData) => void
  onDeleteComment: (entry: EntryData, reply: ReplyData) => void
  onPublicComment: (entry: EntryData, reply: ReplyData) => void
  // Opens the timeline of the person behind a name; only the owner is given the accounts that make this possible.
  onPerson?: (account: string, name: string) => void
  // detail shows the whole post on its own page; in the list a click opens it.
  detail?: boolean
  onOpen?: (entry: EntryData) => void
}

export default function ActivityCard({
  entry,
  onImage,
  onDelete,
  onPublic,
  onShare,
  onDeleteComment,
  onPublicComment,
  onPerson,
  detail,
  onOpen,
}: { entry: EntryData } & Actions) {
  const canManage = entry.activityId > 0
  const visibilityAction = entry.public ? '已公开' : '未公开'
  const quote = entry.quote
  const replies = entry.replies ?? []
  const post = entry.post
  const title = titleOf(entry)
  const time = timeOf(entry)
  const gallery = entry.type === 'Create' && post ? galleryOf(post) : null
  return (
    <>
      <article
        className={'entry' + (detail ? ' entry--detail' : '') + (onOpen ? ' entry--link' : '')}
        id={entry.id}
        data-ref={entry.ref}
        onClick={(event) => {
          if (!onOpen || window.getSelection()?.toString()) return
          if ((event.target as HTMLElement).closest('a, button, video, svg, input')) return
          onOpen(entry)
        }}
      >
        {/* The post answered is a block of its own on top, then the owner's reply under it, as in a thread on X. */}
        {entry.replyTo && (
          <PostCard post={entry.replyTo} onImage={onImage} onPerson={onPerson} reply />
        )}
        <div className="entry-rest">
          <div className="entry-more">
            {(entry.gone || entry.dropped) && (
              <span
                className="entry-gone"
                data-tip={entry.gone ? '原帖已在平台上被删除' : '你已在平台上取消了这条'}
                role="img"
                aria-label={entry.gone ? '原帖已被删除' : '已在平台上取消'}
              >
                <CloudExclamationPoint aria-hidden="true" />
              </span>
            )}
            {canManage && (
              <button
                className="entry-more-button"
                type="button"
                aria-label={entry.public ? '已公开' : '未公开'}
                aria-pressed={entry.public}
                data-tip={visibilityAction}
                onClick={() => onPublic(entry)}
              >
                {entry.public ? (
                  <Globe className="entry-visibility-icon" strokeWidth={2} aria-hidden="true" />
                ) : (
                  <GlobeOff className="entry-visibility-icon" strokeWidth={2} aria-hidden="true" />
                )}
              </button>
            )}
            {canManage && (entry.gone || entry.dropped) && (
              <button
                className="entry-more-button"
                type="button"
                aria-label="删除"
                data-tip="删除"
                onClick={() => {
                  if (window.confirm('确定要从时间线移除这条内容吗？')) onDelete(entry)
                }}
              >
                <Trash2 aria-hidden="true" />
              </button>
            )}
            {(canManage || entry.public) && (
              <button
                className="entry-more-button"
                type="button"
                aria-label="分享"
                data-tip="分享"
                onClick={() => onShare(entry)}
              >
                <Share className="entry-share-icon" strokeWidth={2} aria-hidden="true" />
              </button>
            )}
          </div>
          <div className="entry-head">
            <Name
              className="entry-name"
              name={entry.actor}
              account={entry.actorId}
              onPerson={onPerson}
            />
            <Verb action={verbOf(entry)} />
            {time &&
              (entry.url ? (
                <a className="entry-when" href={entry.url} target="_blank" rel="noopener">
                  <time dateTime={time}>{formatWhen(time)}</time>
                </a>
              ) : (
                <time className="entry-when" dateTime={time}>
                  {formatWhen(time)}
                </time>
              ))}
          </div>
          {title && (
            <p
              className={
                'entry-title' +
                (post?.content ? '' : ' entry-title--only') +
                (post?.markdown ? ' entry-title--article' : '')
              }
            >
              {title}
            </p>
          )}
          {post && <Content post={post} clamp={!detail} />}
          {quote && <PostCard post={quote} onImage={onImage} onPerson={onPerson} />}
          {gallery && (
            <Media
              images={gallery.images}
              videos={gallery.videos}
              livePhotos={gallery.livePhotos}
              onImage={onImage}
            />
          )}
        </div>
      </article>
      {detail && replies.length > 0 && (
        // On a post's own page the comments are a block of their own under the post.
        <section className="replies" aria-label="评论">
          {replies.map((reply, index) => (
            <ReplyThread
              key={index}
              reply={reply}
              source={entry.id}
              onDelete={(gone) => onDeleteComment(entry, gone)}
              onPublic={canManage ? (reply) => onPublicComment(entry, reply) : undefined}
              onPerson={onPerson}
            />
          ))}
        </section>
      )}
    </>
  )
}

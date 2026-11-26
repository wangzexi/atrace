// A comment with its nested replies, on a post's own page.
import { useState } from 'react'
import { CloudExclamationPoint, Ellipsis, Globe, GlobeOff } from 'lucide-react'
import type { Reply as ReplyData } from '../../api'
import Name from './Name'
import { ContentText } from './Content/Content'
import { formatDate } from './time'

// How many levels a thread is set in. Below that the replies stay at the last indent, as on Reddit, so a
// long chain does not squeeze its text into a column; there they say who they answer, which the place
// no longer does.

// One comment in the thread of a post. The thread is one flat list; a reply names whom it answers.
export default function ReplyThread({
  reply,
  source,
  onDelete,
  onPublic,
  onPerson,
}: {
  reply: ReplyData
  source: string
  onDelete: (reply: ReplyData) => void
  // The owner's own reply has its own entry, so it can be made public or private on its own.
  onPublic?: (reply: ReplyData) => void
  onPerson?: (account: string, name: string) => void
}) {
  const [menu, setMenu] = useState(false)
  return (
    <div className="reply">
      <p className="reply-head">
        <Name
          className="entry-reply-author"
          name={reply.author}
          account={reply.authorId}
          onPerson={onPerson}
        />
        {reply.replyTo && <span className="reply-to">回复 {reply.replyTo}</span>}
        {reply.published && (
          <time className="reply-time" dateTime={reply.published}>
            {formatDate(reply.published)}
          </time>
        )}
      {onPublic && (
            <button
              className="entry-more-button"
              type="button"
              aria-label={reply.public ? '已公开' : '未公开'}
              aria-pressed={reply.public}
              data-tip={reply.public ? '设为不公开' : '设为公开'}
              onClick={() => onPublic(reply)}
            >
              {reply.public ? (
                <Globe className="entry-visibility-icon" strokeWidth={2} aria-hidden="true" />
              ) : (
                <GlobeOff className="entry-visibility-icon" strokeWidth={2} aria-hidden="true" />
              )}
            </button>
      )}
        {reply.gone && (
          // The owner sees a comment its author deleted on the platform, and may delete it here.
          <span className="reply-gone">
            <span
              className="entry-gone"
              data-tip="这条评论已在平台上被删除"
              role="img"
              aria-label="评论已被删除"
            >
              <CloudExclamationPoint aria-hidden="true" />
            </span>
            <button
              className="entry-more-button"
              type="button"
              aria-label="更多"
              aria-expanded={menu}
              onClick={() => setMenu(!menu)}
              onBlur={() => setTimeout(() => setMenu(false), 120)}
            >
              <Ellipsis aria-hidden="true" />
            </button>
            {menu && (
              <div className="entry-menu" role="menu">
                <button
                  type="button"
                  role="menuitem"
                  className="danger"
                  onClick={() => {
                    setMenu(false)
                    onDelete(reply)
                  }}
                >
                  删除
                </button>
              </div>
            )}
          </span>
        )}
      </p>
      {reply.content && (
        <p className="reply-text">
          <ContentText source={source} text={reply.content} />
        </p>
      )}
    </div>
  )
}

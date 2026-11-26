// Text of a post, cut after six lines with a 显示更多 button.
import { useEffect, useLayoutEffect, useRef, useState, type ComponentType } from 'react'
import type { Post } from '../../../api'
import Default, { html as defaultHTML, type TextProps } from './Default'
import { renderMarkdown } from './markdown'

const EXCERPT_HEIGHT = 120 // six lines of 20px

// Text of one entry. Long text is cut after six lines, with no ellipsis, and a 显示更多 button
// opens it for good, as X does. The cut falls under the last line that has text: when the sixth
// line is blank, the excerpt ends after the fifth.
export default function Content({
  post,
  clamp,
  more = true,
}: {
  post: Post
  clamp: boolean
  more?: boolean
}) {
  const renderer = useRenderer(post.id)
  const Text = renderer.default
  const html = post.markdown ? renderer.html(renderMarkdown(post).html) : null
  const ref = useRef<HTMLDivElement>(null)
  const [open, setOpen] = useState(false)
  const [excerpt, setExcerpt] = useState<number | null>(null)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el || open || !clamp) return
    const top = el.getBoundingClientRect().top
    const range = document.createRange()
    range.selectNodeContents(el)
    let bottom = 0
    for (const rect of range.getClientRects()) {
      if (rect.width > 0 && rect.height > 0 && rect.top - top < EXCERPT_HEIGHT - 1)
        bottom = Math.max(bottom, rect.bottom - top)
    }
    const height = Math.min(EXCERPT_HEIGHT, Math.ceil(bottom))
    setExcerpt(el.scrollHeight > height + 1 ? height : null)
  }, [post.content, renderer, open, clamp])
  if (!post.content) return null
  return (
    <>
      <div
        ref={ref}
        className={'entry-text' + (post.markdown ? ' entry-md' : '')}
        data-clamped={clamp && !open}
        style={clamp && !open && excerpt !== null ? { maxHeight: excerpt } : undefined}
        {...(html === null
          ? { children: <Text text={post.content} /> }
          : { dangerouslySetInnerHTML: { __html: html } })}
      />
      {more && clamp && excerpt !== null && !open && (
        <button className="show-more" type="button" onClick={() => setOpen(true)}>
          显示更多
        </button>
      )}
    </>
  )
}

interface Renderer {
  default: ComponentType<TextProps>
  html: (html: string) => string
}
const fallback: Renderer = { default: Default, html: defaultHTML }
const loaders: Record<string, () => Promise<Renderer>> = {
  weibo: () => import('./Weibo/Weibo'),
  xhs: () => import('./Xiaohongshu/Xiaohongshu'),
  xiaohongshu: () => import('./Xiaohongshu/Xiaohongshu'),
}
const loaded: Record<string, Renderer> = {}
function useRenderer(id: string): Renderer {
  const platform = id.split(':')[0]
  const [, redraw] = useState(0)
  useEffect(() => {
    const load = loaders[platform]
    if (!load || loaded[platform]) return
    let active = true
    void load()
      .then((renderer) => {
        loaded[platform] = renderer
        if (active) redraw((count) => count + 1)
      })
      .catch(() => {})
    return () => {
      active = false
    }
  }, [platform])
  return loaded[platform] ?? fallback
}

export function ContentText({ source, text }: { source: string; text: string }) {
  const Text = useRenderer(source).default
  return <Text text={text} />
}

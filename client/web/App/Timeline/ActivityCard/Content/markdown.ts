// Markdown of an article, as HTML. The text is someone else's data: raw HTML in it is dropped and
// unsafe link schemes are refused (markdown-it's defaults, kept on purpose). Pictures appear only
// when the archive holds a copy, never as a hotlink; the others show their alt text.
import MarkdownIt from 'markdown-it'
import mark from 'markdown-it-mark'
import type { Post } from '../../../api'

type Env = {
  attachments: Record<string, string>
  inlined: Set<string>
}

const md = new MarkdownIt({ html: false, breaks: true, linkify: true }).use(mark)
// Only addresses with a scheme are links, as with GFM; a bare "example.com" is text.
md.linkify.set({ fuzzyLink: false, fuzzyEmail: false })

const openLink =
  md.renderer.rules.link_open ??
  ((tokens, idx, options, _env, self) => self.renderToken(tokens, idx, options))
md.renderer.rules.link_open = (tokens, idx, options, env, self) => {
  tokens[idx].attrSet('target', '_blank')
  tokens[idx].attrSet('rel', 'noopener noreferrer')
  return openLink(tokens, idx, options, env, self)
}

md.renderer.rules.image = (tokens, idx, _options, rendering) => {
  const env = rendering as Env
  const token = tokens[idx]
  const local = findLocal(String(token.attrGet('src') ?? ''), env.attachments)
  if (!local) return md.utils.escapeHtml(token.content)
  env.inlined.add(local)
  return `<img src="${md.utils.escapeHtml(local)}" alt="${md.utils.escapeHtml(token.content)}">`
}

// An image address in the Markdown is either the picture's URL, or a path relative to the article,
// which ends the URL's path.
function findLocal(src: string, attachments: Record<string, string>): string {
  src = src.trim()
  if (attachments[src]) return attachments[src]
  if (/^[a-z][a-z0-9+.-]*:|^\/\//i.test(src)) return ''
  let relative: string
  try {
    relative = '/' + decodeURIComponent(src)
  } catch {
    return ''
  }
  const parts: string[] = []
  for (const part of relative.split('/')) {
    if (part === '..') parts.pop()
    else if (part !== '' && part !== '.') parts.push(part)
  }
  const wanted = '/' + parts.join('/')
  for (const [original, local] of Object.entries(attachments)) {
    try {
      if (decodeURI(new URL(original).pathname).endsWith(wanted)) return local
    } catch {
      // an address that is not a URL names no picture
    }
  }
  return ''
}

export interface Rendered {
  html: string
  // The archive's copies the text shows in place, which the gallery does not show again.
  inlined: Set<string>
}

const cache = new WeakMap<Post, Rendered>()

export function renderMarkdown(post: Post): Rendered {
  let rendered = cache.get(post)
  if (!rendered) {
    const env: Env = { attachments: post.attachments ?? {}, inlined: new Set() }
    rendered = { html: md.render(post.content, env), inlined: env.inlined }
    cache.set(post, rendered)
  }
  return rendered
}

import { describe, expect, it } from 'vitest'
import type { Post } from '../../../api'
import { galleryOf } from '../activity'
import { renderMarkdown } from './markdown'

function post(
  content: string,
  attachments: Record<string, string> = {},
  images: string[] = Object.values(attachments),
): Post {
  return {
    id: 'blog:post:1',
    author: '',
    title: '',
    content,
    markdown: true,
    published: '',
    url: '',
    images,
    videos: [],
    attachments,
  }
}
const html = (content: string, attachments?: Record<string, string>) =>
  renderMarkdown(post(content, attachments)).html

describe('Markdown of an article', () => {
  it('renders the usual elements', () => {
    const out = html(
      '## 标题\n\n**粗** and `code`\n\n```go\nx := 1 < 2\n```\n\n> 引用\n\n- 一\n- 二\n\n| a | b |\n|---|---|\n| 1 | 2 |\n',
    )
    for (const want of [
      '<h2>标题</h2>',
      '<strong>粗</strong>',
      '<code>code</code>',
      '<pre><code',
      'x := 1 &lt; 2',
      '<blockquote>',
      '<li>一</li>',
      '<table>',
    ])
      expect(out).toContain(want)
  })
  it('marks ==highlighted== text', () => {
    const out = html('==高亮== 与 ==**粗高亮**== 与 a==b')
    expect(out).toContain('<mark>高亮</mark>')
    expect(out).toContain('<mark><strong>粗高亮</strong></mark>')
    expect(out).not.toContain('<mark>b')
  })
  it('keeps a line break inside a paragraph', () => {
    expect(html('before\nafter')).toContain('before<br>\nafter')
  })
  it('opens links in a new tab and does not link a bare domain', () => {
    const out = html('[a](https://example.com) and example.com and https://example.org')
    expect(out).toContain(
      '<a href="https://example.com" target="_blank" rel="noopener noreferrer">a</a>',
    )
    expect(out).toContain('href="https://example.org"')
    expect(out).not.toContain('href="http://example.com"')
    expect(out).not.toContain('href="example.com"')
  })
})

describe("Markdown is someone else's data", () => {
  it('drops raw HTML', () => {
    const out = html(
      '<script>alert(1)</script>\n\n<img src=x onerror=alert(1)>\n\n<b onclick="x()">b</b>',
    )
    expect(out).not.toContain('<script')
    expect(out).not.toContain('<img')
    expect(out).not.toContain('<b ')
    expect(out).toContain('&lt;script&gt;')
  })
  it('refuses unsafe link schemes', () => {
    for (const link of [
      'javascript:alert(1)',
      'JaVaScRiPt:alert(1)',
      'vbscript:x',
      'data:text/html,<script>1</script>',
    ]) {
      expect(html(`[x](${link})`)).not.toContain('<a ')
    }
  })
  it('never hotlinks a picture the archive has no copy of', () => {
    const out = html('![an alt](https://evil.example/p.png) ![javascript](javascript:alert(1))')
    expect(out).not.toContain('<img')
    expect(out).toContain('an alt')
  })
})

describe('Pictures in a Markdown text', () => {
  const copies = {
    'https://blog.example/assets/a%20b.png': '/files/aaaaaaaa',
    'https://blog.example/c.png': '/files/cccccccc',
  }
  it("shows the archive's copy of a picture by its URL", () => {
    expect(html('![c](https://blog.example/c.png)', copies)).toContain(
      '<img src="/files/cccccccc" alt="c">',
    )
  })
  it('finds a picture by a path relative to the article', () => {
    expect(html('![x](./assets/a%20b.png)', copies)).toContain('src="/files/aaaaaaaa"')
    expect(html('![x](../assets/a%20b.png)', copies)).toContain('src="/files/aaaaaaaa"')
    expect(html('![x](/c.png)', copies)).toContain('src="/files/cccccccc"')
  })
  it('does not show again in the gallery what the text shows in place', () => {
    const article = post('![c](https://blog.example/c.png)', copies, [
      '/files/aaaaaaaa',
      '/files/cccccccc',
    ])
    expect(galleryOf(article).images).toEqual(['/files/aaaaaaaa'])
  })
})

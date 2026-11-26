import { describe, expect, it } from 'vitest'
import { renderToStaticMarkup } from 'react-dom/server'
import Default from './Default'
import Weibo, { html as weiboHTML } from './Weibo/Weibo'
import Xiaohongshu from './Xiaohongshu/Xiaohongshu'

describe('platform content', () => {
  it('renders Weibo emoji and its aliases using archive file references', () => {
    const result = renderToStaticMarkup(<Weibo text="[傻眼] [淚] [unknown] <script>" />)
    expect(result.match(/<img /g)).toHaveLength(2)
    expect(result).toContain('/files/sha256:')
    expect(result).toContain('[unknown]')
    expect(result).toContain('&lt;script&gt;')
    expect(weiboHTML('<strong>[傻眼]</strong>')).toContain('<strong><img ')
  })
  it('keeps platform emoji maps separate', () => {
    const result = renderToStaticMarkup(<Xiaohongshu text="[微笑R] [傻眼] [淚]" />)
    expect(result.match(/<img /g)).toHaveLength(1)
    expect(result).toContain('alt="[微笑R]"')
    expect(result).toContain('[傻眼] [淚]')
  })
  it('falls back to ordinary escaped text', () => {
    const result = renderToStaticMarkup(<Default text="[微笑R] <script>" />)
    expect(result).not.toContain('<img')
    expect(result).toContain('[微笑R] &lt;script&gt;')
  })
})

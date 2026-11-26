import { renderToStaticMarkup } from 'react-dom/server'
import { expect, it } from 'vitest'
import Media from './Media'

it('plays a Live Photo inside its image slot with the archived still as poster', () => {
  const html = renderToStaticMarkup(
    <Media
      images={['/files/still', '/files/normal']}
      videos={[]}
      livePhotos={{ '/files/still': '/files/motion' }}
      onImage={() => {}}
    />,
  )
  expect(html).toContain('poster="/files/still"')
  expect(html).toContain('src="/files/motion"')
  expect(html).toContain('实况照片 1')
  expect(html).toContain('preload="none"')
  expect(html).toContain('alt="配图 2"')
  expect(html.match(/<video/g)).toHaveLength(1)
  expect(html).not.toContain('entry-videos')
})

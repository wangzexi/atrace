import { test, expect } from '@playwright/test'
import fixture from './fixtures/archive.json'
let publicRef: string
test.beforeAll(async ({ request }) => {
  const collect = { Authorization: 'Bearer test-collector' }
  const admin = { Authorization: 'Bearer test-password' }
  const imageURL = fixture.imageURL
  expect(
    (
      await request.put('/collect/media?url=' + encodeURIComponent(imageURL), {
        headers: { ...collect, 'Content-Type': 'image/png' },
        data: Buffer.from(fixture.imageBase64, 'base64'),
      })
    ).ok(),
  ).toBeTruthy()
  for (const [id, content] of fixture.posts) {
    for (const document of [
      {
        type: 'Note',
        id: 'weibo:post:ci-' + id,
        attributedTo: 'weibo:user:ci',
        content,
        attachment: id === 'public' ? [{ type: 'Image', url: imageURL }] : [],
        published: '2026-11-24T00:00:00Z',
      },
      {
        type: 'Create',
        actor: 'weibo:user:ci',
        object: 'weibo:post:ci-' + id,
        published: '2026-11-24T00:00:00Z',
      },
    ]) {
      const response = await request.post('/collect/ingest', {
        headers: collect,
        data: {
          observed_at: '2026-11-24T00:00:00Z',
          observed_by: 'browser-test',
          document,
        },
      })
      expect(response.ok()).toBeTruthy()
    }
  }
  const person = await request.post('/admin/people', {
    headers: admin,
    data: { name: '测试主人', accounts: ['weibo:user:ci'] },
  })
  expect(person.ok()).toBeTruthy()
  expect(
    (
      await request.put('/admin/owner', {
        headers: admin,
        data: { person: (await person.json()).id },
      })
    ).ok(),
  ).toBeTruthy()
  const entries = (await (await request.get('/api/entries', { headers: admin })).json()).entries
  const shared = entries.find((entry: { id: string }) => entry.id === 'weibo:post:ci-public')
  publicRef = shared.ref
  expect(
    (
      await request.patch(`/admin/entry/${shared.ref}`, {
        headers: admin,
        data: { public: true },
      })
    ).ok(),
  ).toBeTruthy()
})
test('访客浏览公开笔记、配图和详情', async ({ page }) => {
  await page.goto('/')
  await expect(page.getByText('公开的测试笔记 searchable-public', { exact: true })).toBeVisible()
  await expect(page.getByText('仅主人可见 searchable-private', { exact: true })).toHaveCount(0)
  const image = page.getByRole('img', { name: '配图 1', exact: true })
  await expect(image).toBeVisible()
  await expect
    .poll(() => image.evaluate((element) => (element as HTMLImageElement).naturalWidth))
    .toBeGreaterThan(0)
  await page.getByRole('link', { name: '查看配图 1', exact: true }).click()
  await expect(page.getByRole('button', { name: '关闭大图', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '关闭大图', exact: true }).click()
  await page.goto('/' + publicRef)
  await expect(page.getByText('公开的测试笔记 searchable-public', { exact: true })).toBeVisible()
})
test('登录后可见私有笔记，搜索按输入更新', async ({ page }) => {
  await page.goto('/login')
  await page.getByLabel('密码', { exact: true }).fill('test-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page.getByText('仅主人可见 searchable-private', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '筛选和搜索', exact: true }).click()
  await page.getByRole('searchbox', { name: '搜索历史笔记' }).fill('searchable-private')
  await expect(page.getByText('仅主人可见 searchable-private', { exact: true })).toBeVisible()
  await expect(page.getByText('公开的测试笔记 searchable-public', { exact: true })).toHaveCount(0)
})
test('Markdown 在浏览器直接显示', async ({ page }) => {
  const downloads: string[] = []
  page.on('download', (download) => downloads.push(download.suggestedFilename()))
  for (const path of ['/index.md', '/' + publicRef + '.md']) {
    await page.goto(path)
    await expect(page.locator('body')).toContainText('公开的测试笔记 searchable-public')
  }
  expect(downloads).toEqual([])
})

test('平台正文、跨平台引用和视频在页面切换后正常显示', async ({ page, request }) => {
  await page.setViewportSize({ width: 900, height: 500 })
  const collect = { Authorization: 'Bearer test-collector' }
  const admin = { Authorization: 'Bearer test-password' }
  const videoURL = 'https://example.com/fixture-video.mp4'
  const { readFile } = await import('node:fs/promises')
  expect(
    (
      await request.put('/collect/media?url=' + encodeURIComponent(videoURL), {
        headers: { ...collect, 'Content-Type': 'video/mp4' },
        data: await readFile(new URL('./fixtures/video.mp4', import.meta.url)),
      })
    ).ok(),
  ).toBeTruthy()
  const postID = 'weibo:post:render-fixture'
  for (const document of [
    {
      type: 'Note',
      id: 'xiaohongshu:note:quoted-fixture',
      attributedTo: 'xiaohongshu:user:fixture',
      content: '引用的小红书 [微笑R]',
    },
    {
      type: 'Note',
      id: postID,
      attributedTo: 'weibo:user:ci',
      quote: 'xiaohongshu:note:quoted-fixture',
      content:
        'platform-body [傻眼]\n' +
        Array.from({ length: 14 }, (_, i) => `正文第 ${i + 1} 行`).join('\n'),
      attachment: [
        { type: 'Video', url: videoURL },
        { type: 'Image', url: fixture.imageURL },
      ],
      published: '2026-11-25T00:00:00Z',
    },
    { type: 'Create', actor: 'weibo:user:ci', object: postID, published: '2026-11-25T00:00:00Z' },
    {
      type: 'Note',
      id: 'weibo:comment:render-fixture',
      attributedTo: 'weibo:user:ci',
      inReplyTo: postID,
      content: '评论表情 [淚]',
    },
  ]) {
    expect(
      (
        await request.post('/collect/ingest', {
          headers: collect,
          data: { document, observed_by: 'browser-test', observed_at: '2026-11-25T00:00:00Z' },
        })
      ).ok(),
    ).toBeTruthy()
  }
  const entries = (await (await request.get('/api/entries', { headers: admin })).json()).entries
  const entry = entries.find((item: { id: string }) => item.id === postID)
  expect(
    (
      await request.patch(`/admin/entry/${entry.ref}`, {
        headers: admin,
        data: { public: true },
      })
    ).ok(),
  ).toBeTruthy()
  await page.route('**/files/sha256:*', async (route) => {
    // Platform emoji are fixture images; video range requests still reach the local service.
    if (route.request().resourceType() === 'image') {
      await route.fulfill({
        contentType: 'image/png',
        body: Buffer.from(fixture.imageBase64, 'base64'),
      })
    } else await route.continue()
  })
  await page.goto('/login')
  await page.getByLabel('密码').fill('test-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  const card = page.locator('article.entry').filter({ hasText: 'platform-body' })
  await expect(card.locator('img[alt="[傻眼]"]')).toBeVisible()
  await expect(card.locator('.entry-quote img[alt="[微笑R]"]')).toBeVisible()
  await expect(card.locator('.entry-text').first()).toHaveAttribute('data-clamped', 'true')
  await card.getByRole('button', { name: '显示更多', exact: true }).click()
  await expect(card.locator('.entry-text').first()).toHaveAttribute('data-clamped', 'false')
  await expect
    .poll(() => card.locator('video').evaluate((video: HTMLVideoElement) => video.readyState))
    .toBeGreaterThanOrEqual(1)
  await card.getByRole('link', { name: '查看配图 1', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '查看配图', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '关闭大图', exact: true }).click()
  await expect(page.getByRole('dialog', { name: '查看配图', exact: true })).not.toBeVisible()
  await page.getByRole('button', { name: '筛选和搜索', exact: true }).click()
  await page.getByRole('searchbox', { name: '搜索历史笔记' }).fill('platform-body')
  await expect(page.locator('article.entry')).toHaveCount(1)
  await page.getByRole('button', { name: '筛选和搜索', exact: true }).click()
  await page.getByRole('link', { name: '设置', exact: true }).click()
  await expect(page.getByRole('heading', { name: '设置', exact: true })).toBeVisible()
  await page.getByRole('button', { name: '返回', exact: true }).click()
  await page.getByRole('button', { name: '筛选和搜索', exact: true }).click()
  await expect(page.getByRole('searchbox', { name: '搜索历史笔记' })).toHaveValue('platform-body')
  await page.getByRole('button', { name: '筛选和搜索', exact: true }).click()
  const savedScroll = await page.evaluate(() => {
    window.scrollTo(0, 140)
    return window.scrollY
  })
  expect(savedScroll).toBeGreaterThan(0)
  await card.evaluate((element) => (element as HTMLElement).click())
  await expect(page).toHaveURL(new RegExp('/' + entry.ref + '$'))
  await expect(card.locator('.entry-text').first()).toHaveAttribute('data-clamped', 'false')
  await expect(page.locator('.reply-text img[alt="[淚]"]')).toBeVisible()
  await page.getByRole('button', { name: '返回', exact: true }).click()
  await expect(page.locator('article.entry')).toHaveCount(1)
  // Media and fonts can grow the list after it shows; the reader is still where they were.
  await expect.poll(() => page.evaluate(() => window.scrollY)).toBe(savedScroll)
})

test('公开状态可组合搜索、刷新保留，修改状态后及时移出筛选结果', async ({ page }) => {
  await page.goto('/login')
  await page.getByLabel('密码').fill('test-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await page.getByRole('button', { name: '筛选和搜索', exact: true }).click()
  const visibility = page.getByRole('group', { name: '公开状态', exact: true })
  await expect(visibility.getByRole('button', { name: '全部', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await page.getByRole('searchbox', { name: '搜索历史笔记' }).fill('searchable-')
  await visibility.getByRole('button', { name: '已公开', exact: true }).click()
  await expect(page.getByText('公开的测试笔记 searchable-public', { exact: true })).toBeVisible()
  await expect(page.getByText('仅主人可见 searchable-private', { exact: true })).toHaveCount(0)
  await visibility.getByRole('button', { name: '未公开', exact: true }).click()
  await expect(page.getByText('仅主人可见 searchable-private', { exact: true })).toBeVisible()
  await expect(page.getByText('公开的测试笔记 searchable-public', { exact: true })).toHaveCount(0)
  await page.reload()
  await expect(page.getByText('仅主人可见 searchable-private', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '筛选和搜索', exact: true }).click()
  await expect(visibility.getByRole('button', { name: '未公开', exact: true })).toHaveAttribute(
    'aria-pressed',
    'true',
  )
  await visibility.getByRole('button', { name: '已公开', exact: true }).click()
  await page.getByRole('button', { name: '筛选和搜索', exact: true }).click()
  const card = page.locator('article.entry').filter({ hasText: '公开的测试笔记 searchable-public' })
  await card.getByRole('button', { name: '已公开', exact: true }).click()
  await expect(card).toHaveCount(0)
  await page.getByRole('button', { name: '筛选和搜索', exact: true }).click()
  await visibility.getByRole('button', { name: '未公开', exact: true }).click()
  await expect(page.getByText('公开的测试笔记 searchable-public', { exact: true })).toBeVisible()
})

test('访客忽略上次管理会话保存的未公开筛选', async ({ page }) => {
  await page.addInitScript(() =>
    sessionStorage.setItem(
      'atrace.filters',
      JSON.stringify({ choice: 'Create', visibility: 'private' }),
    ),
  )
  await page.goto('/')
  await expect.poll(() => page.locator('article.entry').count()).toBeGreaterThan(0)
  await expect(page.getByText('仅主人可见 searchable-private', { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: '筛选和搜索', exact: true }).click()
  await expect(page.getByRole('group', { name: '公开状态', exact: true })).toHaveCount(0)
  await expect
    .poll(() =>
      page.evaluate(() => JSON.parse(sessionStorage.getItem('atrace.filters') || '{}').visibility),
    )
    .toBe('all')
})

test('网页终端保留当前位置，登录后能查到草稿', async ({ page }) => {
  const draft = 'archive-private-draft-marker'
  const upload = await page.request.put('/admin/attachments', {
    headers: { Authorization: 'Bearer test-password' },
    data: { object: 'weibo:post:ci-public', files: [{ path: 'refs/drafts/草稿.md', content: draft }] },
  })
  expect(upload.status()).toBe(204)
  await page.goto('/')
  await page.getByRole('link', { name: '档案终端' }).click()
  const input = page.getByRole('textbox', { name: '命令', exact: true })
  const log = page.getByRole('log')
  const run = async (command: string) => {
    await input.fill(command)
    await input.press('Enter')
    await expect(page.getByRole('button', { name: '运行', exact: true })).toBeEnabled()
  }
  const path = '/atrace/测试主人/2026-11/' + publicRef
  const visitorPath = '/atrace/2026-11/' + publicRef
  await run('cd ' + visitorPath)
  await run('pwd')
  await expect(log).toContainText(visitorPath + '\n')
  await run('grep -rl ' + draft + ' .')
  await expect(log).not.toContainText('attachments/refs/drafts')
  const login = await page.request.post('/login', { form: { password: 'test-' + 'password' } })
  expect(login.ok()).toBeTruthy()
  await run('cd ' + path)
  await run('grep -rl ' + draft + ' .')
  await expect(log).toContainText(path + '/attachments/refs/drafts/草稿.md')
})

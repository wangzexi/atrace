import { test, expect } from '@playwright/test'

test('人物可以拖动和键盘排序，保存失败恢复原顺序', async ({ page }) => {
  let people = [
    { id: 101, name: '甲', owner: true, accounts: [{ id: 'weibo:user:1', name: '' }, { id: 'x:user:1', name: '' }] },
    { id: 102, name: '乙', owner: false, accounts: [] },
    { id: 103, name: '丙', owner: false, accounts: [] },
  ]
  let fail = false
  await page.route('**/admin/people', route => route.fulfill({ json: { people, offered: [] } }))
  await page.route('**/admin/people/order', async route => {
    if (fail) return route.fulfill({ status: 500, json: { error: { message: '保存失败' } } })
    const ids = route.request().postDataJSON().people as number[]
    people = ids.map((id, index) => ({ ...people.find(p => p.id === id)!, owner: index === 0 }))
    await route.fulfill({ status: 204 })
  })
  await page.goto('/login')
  await page.getByLabel('密码', { exact: true }).fill('test-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL('/')
  await page.goto('/settings/people/')
  const rows = page.getByRole('list', { name: '人物排序' }).locator(':scope > li')
  await expect(rows.locator('summary strong')).toHaveText(['甲', '乙', '丙'])
  await expect(rows.first().locator('small')).toHaveText('weibo、x')
  await page.getByRole('button', { name: '调整乙的顺序' }).press('ArrowUp')
  await expect(rows.locator('summary strong')).toHaveText(['乙', '甲', '丙'])
  await expect(page.getByRole('list', { name: '人物排序' })).toHaveAttribute('aria-busy', 'false')
  await page.getByRole('button', { name: '调整丙的顺序' }).scrollIntoViewIfNeeded()
  const grip = await page.getByRole('button', { name: '调整丙的顺序' }).boundingBox()
  const first = await rows.first().boundingBox()
  if (!grip || !first) throw Error('missing drag target')
  await page.mouse.move(grip.x + grip.width / 2, grip.y + grip.height / 2)
  await page.mouse.down()
  await page.mouse.move(first.x + 16, first.y + 2, { steps: 10 })
  await expect(page.locator('.person--dragging')).toHaveCount(1)
  await expect(page.locator('.person--drop-before')).toHaveCount(1)
  await page.mouse.up()
  await expect(rows.locator('summary strong')).toHaveText(['丙', '乙', '甲'])
  await expect(page.getByRole('list', { name: '人物排序' })).toHaveAttribute('aria-busy', 'false')
  await page.reload()
  await expect(rows.locator('summary strong')).toHaveText(['丙', '乙', '甲'])
  fail = true
  await page.getByRole('button', { name: '调整丙的顺序' }).press('ArrowDown')
  await expect(page.getByRole('status')).toBeVisible()
  await expect(rows.locator('summary strong')).toHaveText(['丙', '乙', '甲'])
})

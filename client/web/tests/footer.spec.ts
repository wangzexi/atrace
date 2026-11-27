import { test, expect } from '@playwright/test'

test('主人可以保存和清空首页页脚', async ({ page, request }) => {
  const write = await request.put('/admin/settings/footer', { data: { html: '访客修改' } })
  expect(write.status()).toBe(401)

  await page.goto('/')
  await expect(page.locator('footer.site-footer')).toHaveCount(0)
  await page.goto('/login')
  await page.getByLabel('密码', { exact: true }).fill('test-password')
  await page.getByRole('button', { name: '登录', exact: true }).click()
  await expect(page).toHaveURL('/')
  await page.goto('/settings/site/')

  const html = page.getByLabel('页脚 HTML', { exact: true })
  const save = page.getByRole('button', { name: '保存页脚' })
  const safe = '<a href="https://example.org/filing"><strong>示例备案号</strong></a>'
  await html.fill(
    '<a href="https://example.org/filing" onclick="return false"><strong>示例备案号</strong></a>' +
      '<script>unsafe</script><a href="javascript:void(0)">纯文字</a>',
  )
  await save.click()
  await expect(page.getByText('页脚已保存')).toBeVisible()

  await page.goto('/')
  const footer = page.locator('footer.site-footer')
  await expect(footer.getByRole('link', { name: '示例备案号' })).toHaveAttribute(
    'href',
    'https://example.org/filing',
  )
  await expect(footer.locator('[onclick], script')).toHaveCount(0)
  await expect(footer.getByRole('link')).toHaveCount(1)
  await page.goto('/settings/site/')
  await expect(html).toHaveValue(safe + '纯文字')
  await html.fill('')
  await save.click()
  await expect(page.getByText('页脚已保存')).toBeVisible()
  await page.goto('/')
  await expect(footer).toHaveCount(0)
})

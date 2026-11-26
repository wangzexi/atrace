import { defineConfig } from '@playwright/test'
import { mkdtempSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
const port = process.env.ATRACE_TEST_PORT
if (!port) throw new Error('Run browser tests through .gitea/ci.sh')
const baseURL = 'http://127.0.0.1:' + port
const directory = mkdtempSync(join(tmpdir(), 'atrace-browser-'))
export default defineConfig({
  testDir: './tests',
  workers: 1,
  retries: 0,
  outputDir: process.env.ATRACE_TEST_REPORTS + '/browser-results',
  reporter: [
    ['list'],
    ['html', { outputFolder: process.env.ATRACE_TEST_REPORTS + '/browser-report', open: 'never' }],
  ],
  use: {
    baseURL,
    browserName: 'chromium',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  webServer: {
    command:
      "printf 'test-password\\n' | ../../server/build/atrace-server set-password && ../../server/build/atrace-server",
    url: baseURL + '/healthz',
    reuseExistingServer: false,
    env: {
      ATRACE_ADDR: '127.0.0.1:' + port,
      ATRACE_DATABASE: join(directory, 'test.db'),
      ATRACE_FILES: join(directory, 'files'),
      ATRACE_WEB_BUILD: join(process.cwd(), 'build'),
      ATRACE_COLLECT_TOKEN: 'test-collector',
    },
  },
})

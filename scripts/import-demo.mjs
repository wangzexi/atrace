import { env } from 'node:process'

const base = env.ATRACE_SERVER || 'http://127.0.0.1:8183'
const token = env.ATRACE_COLLECT_TOKEN
if (!token) throw new Error('Set ATRACE_COLLECT_TOKEN before importing the sample')

const actor = 'demo:user:reader'
const object = 'demo:post:first'
const documents = [
  { type: 'Person', id: actor, name: 'Demo Reader' },
  { type: 'Note', id: object, attributedTo: actor, content: '这是我的第一条档案记录。' },
  { type: 'Create', actor, object },
]
const observed_at = new Date().toISOString()
for (const document of documents) {
  const response = await fetch(new URL('/collect/ingest', base), {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
    body: JSON.stringify({ observed_at, observed_by: 'demo', document }),
    redirect: 'error',
    signal: AbortSignal.timeout(15_000),
  })
  if (!response.ok) throw new Error(`Import failed with HTTP ${response.status}: ${await response.text()}`)
}
console.log(`已导入样例。将账号 ${actor} 关联到站点默认人物，即可在时间线看到它。`)

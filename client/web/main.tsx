import { createRoot } from 'react-dom/client'
import App from './App/App'

createRoot(document.getElementById('root')!).render(<App />)

// When atrace runs with ATRACE_DEV, reload once the server restarts with a new build.
if (import.meta.env.PROD) {
  void (async () => {
    let boot: string | undefined
    for (;;) {
      try {
        const response = await fetch('/dev/boot', { cache: 'no-store' })
        if (!response.ok) return
        const now = await response.text()
        if (boot && now !== boot) location.reload()
        boot = now
      } catch {
        // the server is restarting
      }
      await new Promise((resolve) => setTimeout(resolve, 1000))
    }
  })()
}

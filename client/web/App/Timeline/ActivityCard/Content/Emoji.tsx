import type { ReactNode } from 'react'

type Emojis = Record<string, string>
const TOKEN = /\[([^[\]<>&"']{1,20})\]/g

export function emojiRenderer(
  emojis: Emojis,
  aliases: Record<string, string> = {},
): { text: (text: string) => ReactNode[]; html: (html: string) => string } {
  return {
    text: (text) => {
      const nodes: ReactNode[] = []
      let last = 0
      for (const match of text.matchAll(TOKEN)) {
        const src = emojis[match[1]] ?? emojis[aliases[match[1]]]
        if (!src) continue
        nodes.push(text.slice(last, match.index))
        nodes.push(
          <img
            key={match.index}
            className="entry-emoji"
            src={`/files/${src}`}
            alt={match[0]}
            title={match[0]}
            width={20}
            height={20}
            loading="lazy"
          />,
        )
        last = match.index + match[0].length
      }
      nodes.push(text.slice(last))
      return nodes
    },
    html: (html) => {
      return html.replace(TOKEN, (token, name: string) => {
        const src = emojis[name] ?? emojis[aliases[name]]
        return src
          ? `<img class="entry-emoji" src="/files/${src}" alt="${token}" title="${token}" width="20" height="20" loading="lazy">`
          : token
      })
    },
  }
}

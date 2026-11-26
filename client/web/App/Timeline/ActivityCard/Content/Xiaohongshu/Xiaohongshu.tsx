import emojis from './emoji.json'
import { emojiRenderer } from '../Emoji'
import type { TextProps } from '../Default'

const renderer = emojiRenderer(emojis)
export default function Xiaohongshu({ text }: TextProps) {
  return <>{renderer.text(text)}</>
}
export const html = renderer.html

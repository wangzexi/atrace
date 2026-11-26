import emojis from './emoji.json'
import { emojiRenderer } from '../Emoji'
import type { TextProps } from '../Default'

const renderer = emojiRenderer(emojis, { 黑線: '黑线', 淚: '泪' })
export default function Weibo({ text }: TextProps) {
  return <>{renderer.text(text)}</>
}
export const html = renderer.html

export interface TextProps {
  text: string
}

export default function Default({ text }: TextProps) {
  return <>{text}</>
}
export const html = (text: string) => text

// Times arrive as ISO strings; the page shows them in the viewer's own time zone.

const two = (n: number) => String(n).padStart(2, '0')

// The way a timeline header shows a time: the date, with the year only when it is not this year, then hour and minute.
export function formatWhen(iso: string): string {
  const date = new Date(iso)
  if (!iso || Number.isNaN(date.getTime())) return ''
  const year = date.getFullYear() === new Date().getFullYear() ? '' : `${date.getFullYear()}年`
  return `${year}${date.getMonth() + 1}月${date.getDate()}日 ${two(date.getHours())}:${two(date.getMinutes())}`
}

// A comment's date.
export function formatDate(iso: string): string {
  const date = new Date(iso)
  if (!iso || Number.isNaN(date.getTime())) return ''
  return `${date.getFullYear()}/${date.getMonth() + 1}/${date.getDate()}`
}

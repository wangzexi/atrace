import { ArrowLeft } from 'lucide-react'
import type { MouseEventHandler, ReactNode } from 'react'

export default function PageHeader({
  title,
  subpage,
  onBack,
  onTitleClick,
  children,
}: {
  title: string
  subpage: boolean
  onBack: () => void
  onTitleClick?: MouseEventHandler<HTMLHeadingElement>
  children?: ReactNode
}) {
  return (
    <header className={'intro' + (subpage ? ' intro--detail' : ' intro--list')}>
      <button
        className="back"
        type="button"
        aria-label="返回"
        data-tip="返回"
        tabIndex={subpage ? 0 : -1}
        aria-hidden={!subpage}
        onClick={onBack}
      >
        <ArrowLeft aria-hidden="true" />
      </button>
      <h1
        className={'notes-title' + (onTitleClick ? '' : ' notes-title--static')}
        onClick={onTitleClick}
      >
        {title}
      </h1>
      {children}
    </header>
  )
}

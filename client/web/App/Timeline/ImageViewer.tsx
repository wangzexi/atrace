import { useEffect, useRef } from 'react'
import { ChevronLeft, ChevronRight, X } from 'lucide-react'
export interface Viewing {
  images: string[]
  index: number
}
export default function ImageViewer({
  viewing,
  onClose,
  onStep,
}: {
  viewing: Viewing | null
  onClose: () => void
  onStep: (by: number) => void
}) {
  const viewer = useRef<HTMLDialogElement>(null)
  useEffect(() => {
    const dialog = viewer.current
    if (!dialog) return
    if (viewing && !dialog.open) dialog.showModal()
    // The page behind a modal viewer must not scroll.
    document.body.style.overflow = viewing ? 'hidden' : ''
    return () => {
      document.body.style.overflow = ''
    }
  }, [viewing])

  return (
    <dialog
      ref={viewer}
      className="image-viewer"
      aria-label="查看配图"
      onClose={() => onClose()}
      onClick={(event) => event.target === viewer.current && viewer.current?.close()}
      onKeyDown={(event) => {
        if (event.key === 'ArrowLeft') onStep(-1)
        if (event.key === 'ArrowRight') onStep(1)
      }}
    >
      <button
        className="image-viewer-close"
        type="button"
        aria-label="关闭大图"
        onClick={() => viewer.current?.close()}
      >
        <X aria-hidden="true" />
      </button>
      {viewing && (
        <>
          <img src={viewing.images[viewing.index]} alt={`配图 ${viewing.index + 1}`} />
          {viewing.images.length > 1 && (
            <>
              <button
                className="image-viewer-step image-viewer-step--prev"
                type="button"
                aria-label="上一张"
                onClick={() => onStep(-1)}
              >
                <ChevronLeft aria-hidden="true" />
              </button>
              <button
                className="image-viewer-step image-viewer-step--next"
                type="button"
                aria-label="下一张"
                onClick={() => onStep(1)}
              >
                <ChevronRight aria-hidden="true" />
              </button>
              <span className="image-viewer-count">
                {viewing.index + 1} / {viewing.images.length}
              </span>
            </>
          )}
        </>
      )}
    </dialog>
  )
}

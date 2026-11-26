// Pictures and videos of a post or of a quoted post.
export default function Media({
  images,
  videos,
  livePhotos = {},
  onImage,
}: {
  images: string[] | null
  videos: string[] | null
  livePhotos?: Record<string, string>
  onImage: (images: string[], index: number) => void
}) {
  const pictures = (images ?? []).slice(0, 9)
  const clips = (videos ?? []).slice(0, 3)
  return (
    <>
      {pictures.length > 0 && (
        <div className="entry-images" data-count={pictures.length}>
          {pictures.map((src, index) =>
            livePhotos[src] ? (
              <div key={src} className="entry-image entry-live-photo">
                <video
                  src={livePhotos[src]}
                  poster={src}
                  controls
                  playsInline
                  preload="none"
                  aria-label={`实况照片 ${index + 1}`}
                />
                <span className="entry-live-label">LIVE</span>
              </div>
            ) : (
              <a
                key={src}
                className="entry-image"
                href={src}
                aria-label={`查看配图 ${index + 1}`}
                onClick={(event) => {
                  event.preventDefault()
                  onImage(pictures, index)
                }}
              >
                <img
                  src={src}
                  alt={`配图 ${index + 1}`}
                  loading="lazy"
                  referrerPolicy="no-referrer"
                />
              </a>
            ),
          )}
        </div>
      )}
      {clips.length > 0 && (
        <div className="entry-videos">
          {clips.map((src, index) => (
            <video
              key={src}
              className="entry-video"
              controls
              playsInline
              preload="metadata"
              aria-label={`视频 ${index + 1}`}
              src={src}
            />
          ))}
        </div>
      )}
    </>
  )
}

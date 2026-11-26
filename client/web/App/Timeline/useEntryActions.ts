// What the owner can do with an entry: delete with undo, make public, delete a comment, share.
import { useRef, useState } from 'react'
import {
  readEntry,
  setHidden,
  setPublic,
  type Visibility,
  type Entry as EntryData,
} from '../api'
import { matchesVisibility } from './useTimeline'

interface Timeline {
  visibility: Visibility
  entries: EntryData[]
  setEntries: React.Dispatch<React.SetStateAction<EntryData[]>>
  setError: (error: string) => void
}

export function useEntryActions(
  timeline: Timeline,
  setDetail: React.Dispatch<React.SetStateAction<EntryData | null>>,
  postId: string | null,
  back: () => void,
) {
  const { entries, setEntries, setError, visibility } = timeline
  const [sharing, setSharing] = useState<EntryData | null>(null)
  const [notice, setNotice] = useState('')
  const [undo, setUndo] = useState<{ entry: EntryData; index: number } | null>(null)
  const undoTimer = useRef<number>()

  // Deleting hides the entry on the server (soft delete) and offers to take it back for a while.
  async function remove(entry: EntryData) {
    const index = entries.findIndex((item) => item.ref === entry.ref)
    try {
      await setHidden(entry.ref, true)
    } catch (e) {
      setError((e as Error).message)
      return
    }
    setEntries((current) => current.filter((item) => item.ref !== entry.ref))
    setUndo({ entry, index })
    if (postId !== null) back()
    window.clearTimeout(undoTimer.current)
    undoTimer.current = window.setTimeout(() => setUndo(null), 8000)
  }

  async function restore() {
    if (!undo) return
    const { entry, index } = undo
    try {
      await setHidden(entry.ref, false)
    } catch (e) {
      setError((e as Error).message)
      return
    }
    const at = Math.max(0, index)
    if (matchesVisibility(entry, visibility))
      setEntries((current) => [...current.slice(0, at), entry, ...current.slice(at)])
    setUndo(null)
  }

  function replace(updated: EntryData) {
    setEntries((current) =>
      current
        .map((item) => (item.ref === updated.ref ? updated : item))
        .filter((entry) => matchesVisibility(entry, visibility)),
    )
    setDetail((current) => (current?.ref === updated.ref ? updated : current))
  }

  function tell(text: string) {
    setNotice(text)
    window.setTimeout(() => setNotice((now) => (now === text ? '' : now)), 6000)
  }

  // A comment its author deleted on the platform can be deleted here too (a soft delete on the server).
  async function deleteComment(entry: EntryData, reply: { ref?: string }) {
    if (!reply.ref) return
    try {
      await setHidden(reply.ref, true)
      replace(await readEntry(entry.ref))
    } catch (e) {
      tell((e as Error).message)
    }
  }

  // A reply of the owner's is its own entry: it is public on its own, not with the post.
  async function togglePublicComment(entry: EntryData, reply: { ref?: string; public?: boolean }) {
    if (!reply.ref) return
    try {
      await setPublic(reply.ref, !reply.public)
      replace(await readEntry(entry.ref))
    } catch (e) {
      tell((e as Error).message)
    }
  }

  async function togglePublic(entry: EntryData) {
    try {
      await setPublic(entry.ref, !entry.public)
      replace({ ...entry, public: !entry.public })
    } catch (e) {
      tell((e as Error).message)
    }
  }

  return {
    remove,
    restore,
    undo,
    notice,
    deleteComment,
    togglePublic,
    togglePublicComment,
    sharing,
    setSharing,
    replace,
  }
}

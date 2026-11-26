// A name on an entry. For the owner, who is given the account behind it, it opens the person it belongs to.
export default function Name({
  name,
  account,
  className,
  onPerson,
}: {
  name: string
  account?: string
  className: string
  onPerson?: (account: string, name: string) => void
}) {
  if (!onPerson || !account) return <span className={className}>{name}</span>
  return (
    <button
      className={className + ' person-link'}
      type="button"
      data-tip="看Ta的时间线"
      onClick={() => onPerson(account, name)}
    >
      {name}
    </button>
  )
}

import type { Person } from '../api'

// The platform an account belongs to: the start of its ID.
export const platformOf = (id: string) => id.split(':')[0]

// The accounts a person has once another person's are added: each account once, the person's own first.
export function mergedAccounts(into: Person, from: Person): string[] {
  return [...new Set([...into.accounts, ...from.accounts].map((account) => account.id))]
}

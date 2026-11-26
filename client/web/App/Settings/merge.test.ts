import { describe, expect, it } from 'vitest'
import type { Person } from '../api'
import { mergedAccounts } from './merge'

const person = (id: number, ...accounts: string[]): Person => ({
  id,
  name: `p${id}`,
  owner: false,
  accounts: accounts.map((account) => ({ id: account, name: '' })),
})

describe('mergedAccounts', () => {
  it("keeps the person's own accounts first and adds the other's", () => {
    expect(mergedAccounts(person(1, 'weibo:user:1'), person(2, 'x:user:2'))).toEqual([
      'weibo:user:1',
      'x:user:2',
    ])
  })
  it('lists an account both have once', () => {
    expect(
      mergedAccounts(person(1, 'weibo:user:1', 'x:user:2'), person(2, 'x:user:2', 'zhihu:user:3')),
    ).toEqual(['weibo:user:1', 'x:user:2', 'zhihu:user:3'])
  })
  it('loses nothing when one side has no accounts', () => {
    expect(mergedAccounts(person(1), person(2, 'x:user:2'))).toEqual(['x:user:2'])
    expect(mergedAccounts(person(1, 'x:user:2'), person(2))).toEqual(['x:user:2'])
  })
})

import { describe, expect, it } from 'vitest'

import { hasPermission, PERM_DISCONNECT, PERM_EXPORT } from './permissions'

describe('hasPermission (role-derived permission gating, contract C2)', () => {
  it('grants everything to admin', () => {
    expect(hasPermission('admin', 'subscribers.delete')).toBe(true)
    expect(hasPermission('admin', PERM_EXPORT)).toBe(true)
    expect(hasPermission('admin', 'anything.at.all')).toBe(true)
  })

  it('grants operators the frozen operator set and denies the rest', () => {
    expect(hasPermission('operator', 'subscribers.edit')).toBe(true)
    expect(hasPermission('operator', PERM_DISCONNECT)).toBe(true)
    expect(hasPermission('operator', PERM_EXPORT)).toBe(true)
    // Not in the operator set:
    expect(hasPermission('operator', 'subscribers.delete')).toBe(false)
    expect(hasPermission('operator', 'nas.create')).toBe(false)
    expect(hasPermission('operator', 'managers.view')).toBe(false)
  })

  it('limits agents to view + renew', () => {
    expect(hasPermission('agent', 'subscribers.view')).toBe(true)
    expect(hasPermission('agent', 'renew')).toBe(true)
    expect(hasPermission('agent', 'subscribers.edit')).toBe(false)
    expect(hasPermission('agent', PERM_DISCONNECT)).toBe(false)
    expect(hasPermission('agent', PERM_EXPORT)).toBe(false)
  })

  it('denies when the role is unknown or missing', () => {
    expect(hasPermission(undefined, 'subscribers.view')).toBe(false)
    expect(hasPermission('', 'subscribers.view')).toBe(false)
    expect(hasPermission('mystery', 'subscribers.view')).toBe(false)
  })
})

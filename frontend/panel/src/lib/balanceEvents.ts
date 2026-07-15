/**
 * Cross-component "a manager balance changed" signal (item 7: the header
 * balance must update without a full page reload). Balance-affecting API
 * calls fire this after success; BalanceWidget (and anything else showing a
 * balance) listens and refetches. Server-initiated changes (an admin topping
 * you up from another session) are covered by the widget's focus/interval
 * refresh, not this event.
 */
export const BALANCE_CHANGED_EVENT = 'hikrad:balance-changed'

export function notifyBalanceChanged(): void {
  window.dispatchEvent(new Event(BALANCE_CHANGED_EVENT))
}

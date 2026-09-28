/**
 * v4-9 (FR-153-159): double-entry accounting, invoices/expenses, undo, and
 * the agent approval gate. Response shapes mirror internal/billing exactly.
 */
import { API_BASE, listPage, request, type Page, type PageParams } from './client'
import { tokenStore } from '../auth/tokenStore'

// --- chart of accounts / statements ----------------------------------------

export interface Account {
  id: string
  code: string
  name: string
  type: 'asset' | 'liability' | 'equity' | 'revenue' | 'expense'
  subtype: string | null
  normal_balance: 'debit' | 'credit'
  is_system: boolean
  archived: boolean
}

export function listAccounts(type?: string): Promise<{ items: Account[] }> {
  return request(`/accounts`, { query: { type } })
}

export interface ProfitAndLoss {
  from: string
  to: string
  revenue: number
  /** Absent (not null) for a scoped/reseller caller — gate item 12. */
  cost_of_service?: number
  gross_margin?: number
  commission_expense: number
  operating_expense: number
  depreciation_expense: number
  net_income: number
}

export function getProfitAndLoss(from?: string, to?: string): Promise<ProfitAndLoss> {
  return request('/statements/pl', { query: { from, to } })
}

export interface BalanceSheet {
  as_of: string
  assets: number
  liabilities: number
  capital: number
  retained_earnings: number
  equity: number
}

export function getBalanceSheet(asOf?: string): Promise<BalanceSheet> {
  return request('/statements/balance-sheet', { query: { as_of: asOf } })
}

export interface CashFlowLine {
  account_code: string
  account_name: string
  net_change: number
}

export function getCashFlow(
  from?: string,
  to?: string,
): Promise<{ from: string; to: string; lines: CashFlowLine[]; net_change: number }> {
  return request('/statements/cash-flow', { query: { from, to } })
}

export interface AgingBucket {
  Bucket: string
  Amount: number
}

export function getAgingReceivables(): Promise<{ items: AgingBucket[] }> {
  return request('/statements/aging/receivables')
}

export function getAgingPayables(): Promise<{ items: AgingBucket[] }> {
  return request('/statements/aging/payables')
}

export interface TrialBalanceLine {
  code: string
  name: string
  type: string
  debit: number
  credit: number
}

export function getTrialBalance(asOf?: string): Promise<{
  as_of: string
  lines: TrialBalanceLine[]
  total_debit: number
  total_credit: number
}> {
  return request('/statements/trial-balance', { query: { as_of: asOf } })
}

export interface GeneralLedgerLine {
  at: string
  entry_memo: string
  source: string
  reference: string
  debit: number
  credit: number
  line_memo: string
  running_balance: number
}

export function getGeneralLedger(
  account: string,
  from?: string,
  to?: string,
): Promise<{ account: string; items: GeneralLedgerLine[] }> {
  return request('/statements/general-ledger', { query: { account, from, to } })
}

export interface JournalEntry {
  id: string
  at: string
  memo: string
  source: string
  reference: string
}

export function listJournal(
  params: PageParams = {},
  from?: string,
  to?: string,
): Promise<Page<JournalEntry>> {
  return listPage('/statements/journal', params, { from, to })
}

// --- invoices (FR-154) -------------------------------------------------------

export type InvoiceMode = 'full' | 'prorated' | 'no_charge'
export type DocStatus = 'unpaid' | 'partial' | 'due' | 'paid' | 'void'

export interface InvoiceLine {
  description: string
  category: string
  quantity: number
  unit_price: number
  amount: number
}

export interface Invoice {
  id: string
  invoice_no: string
  subscriber_id: string | null
  mode: InvoiceMode
  issue_date: string
  due_date: string
  tax_rate: number
  subtotal: number
  tax_amount: number
  total: number
  amount_paid: number
  status: DocStatus
  memo: string
  lines?: InvoiceLine[]
}

export interface CreateInvoiceRequest {
  subscriber_id?: string
  mode: InvoiceMode
  due_date: string
  tax_rate?: number
  memo?: string
  lines: { description: string; category?: string; quantity: number; unit_price: number }[]
}

export function listInvoices(
  params: PageParams = {},
  filters: { status?: string; subscriber_id?: string } = {},
): Promise<Page<Invoice>> {
  return listPage('/invoices', params, {
    status: filters.status,
    subscriber_id: filters.subscriber_id,
  })
}

export function getInvoice(id: string): Promise<Invoice> {
  return request(`/invoices/${id}`)
}

export function createInvoice(body: CreateInvoiceRequest): Promise<Invoice> {
  return request('/invoices', { method: 'POST', body })
}

export interface PendingApproval {
  pending_approval: true
  approval_request_id: string
}

export function voidInvoice(
  id: string,
  reason: string,
): Promise<{ id: string; status: string } | PendingApproval> {
  return request(`/invoices/${id}/void`, { method: 'POST', body: { reason } })
}

export function postInvoiceReceipt(
  id: string,
  body: { amount: number; method?: string; memo?: string },
): Promise<{ id: string; invoice_id: string; amount: number }> {
  return request(`/invoices/${id}/receipts`, { method: 'POST', body })
}

// --- expenses (FR-155) -------------------------------------------------------

export interface Expense {
  id: string
  expense_no: string
  category: string
  vendor: string
  amount: number
  amount_paid: number
  incurred_date: string
  due_date: string
  status: DocStatus
  memo: string
}

export function listExpenses(params: PageParams = {}, status?: string): Promise<Page<Expense>> {
  return listPage('/expenses', params, { status })
}

export function createExpense(body: {
  category: string
  vendor?: string
  amount: number
  due_date: string
  memo?: string
}): Promise<Expense> {
  return request('/expenses', { method: 'POST', body })
}

export function voidExpense(id: string, reason: string): Promise<{ id: string; status: string }> {
  return request(`/expenses/${id}/void`, { method: 'POST', body: { reason } })
}

export function postExpensePayment(
  id: string,
  body: { amount: number; method?: string; memo?: string },
): Promise<{ id: string; expense_id: string; amount: number }> {
  return request(`/expenses/${id}/payments`, { method: 'POST', body })
}

// --- fixed assets (FR-157) ---------------------------------------------------

export interface FixedAsset {
  id: string
  name: string
  cost: number
  useful_life_months: number
  salvage_value: number
  acquired_date: string
  accumulated_depreciation: number
}

export function listFixedAssets(): Promise<{ items: FixedAsset[] }> {
  return request('/fixed-assets')
}

export function createFixedAsset(body: {
  name: string
  cost: number
  acquired_date?: string
  useful_life_months: number
  salvage_value?: number
}): Promise<{ id: string }> {
  return request('/fixed-assets', { method: 'POST', body })
}

export function runDepreciation(id: string): Promise<{ posted: boolean }> {
  return request(`/fixed-assets/${id}/depreciate`, { method: 'POST' })
}

// --- undo (FR-158) ------------------------------------------------------------

export interface UndoableAction {
  id: string
  kind: string
  entity_type: string
  entity_id: string
  created_at: string
  expires_at: string
}

export function listMyUndoable(): Promise<{ items: UndoableAction[] }> {
  return request('/undo/mine')
}

export interface UndoResult {
  undo_id: string
  reversal_ledger_tx_id: string
  redo_undo_id: string
}

export function undoAction(id: string): Promise<UndoResult | PendingApproval> {
  return request(`/undo/${id}`, { method: 'POST' })
}

// --- approvals (FR-159) -------------------------------------------------------

export type ApprovalState = 'pending' | 'approved' | 'rejected' | 'withdrawn'

export interface ApprovalRequest {
  id: string
  kind: string
  payload: unknown
  requested_by: string
  description: string
  state: ApprovalState
  decided_by: string | null
  decided_at: string | null
  reject_reason: string | null
  result: unknown
  created_at: string
}

export function listApprovalRequests(
  opts: { state?: string; mine?: boolean } = {},
): Promise<{ items: ApprovalRequest[] }> {
  return request('/approval-requests', {
    query: { state: opts.state, mine: opts.mine ? 'true' : undefined },
  })
}

export function approveRequest(
  id: string,
): Promise<{ id: string; state: string; result: unknown }> {
  return request(`/approval-requests/${id}/approve`, { method: 'POST' })
}

export function rejectRequest(id: string, reason: string): Promise<{ id: string; state: string }> {
  return request(`/approval-requests/${id}/reject`, { method: 'POST', body: { reason } })
}

export function withdrawRequest(id: string): Promise<{ id: string; state: string }> {
  return request(`/approval-requests/${id}/withdraw`, { method: 'POST' })
}

/** Type guard: did a void/undo call park instead of executing (FR-159)? */
export function isPendingApproval(v: unknown): v is PendingApproval {
  return typeof v === 'object' && v !== null && (v as PendingApproval).pending_approval === true
}

export function accountingExportHeaders(): Record<string, string> {
  const token = tokenStore.getAccessToken()
  return token ? { Authorization: `Bearer ${token}` } : {}
}

export const ACCOUNTING_API_BASE = API_BASE

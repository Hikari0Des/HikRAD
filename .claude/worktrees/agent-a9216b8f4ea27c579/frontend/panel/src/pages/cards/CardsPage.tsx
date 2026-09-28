import { useState } from 'react'

import { ErrorState, LoadingState, useFormatters, useT } from '@hikrad/shared'

import {
  addMoreCards,
  cardUsageCSVURL,
  createCardGroup,
  duplicateGroup,
  extendGroup,
  getCardGroup,
  listCardGroups,
  listCards,
  printCardGroup,
  resetCard,
  setCardEnabled,
  setGroupEnabled,
  type Card,
  type CardGroup,
  type CreateGroupInput,
} from '../../api/cards'
import { API_BASE } from '../../api/client'
import { useAuth } from '../../auth/AuthContext'
import { PERM_CARDS_CREATE, PERM_CARDS_EDIT, PERM_CARDS_PRINT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { ConfirmDialog } from '../../components/ConfirmDialog'
import { Field, Select, TextInput } from '../../components/form'
import { Modal } from '../../components/Modal'
import { PageHeader } from '../../components/PageHeader'
import { Pill } from '../../components/Pill'
import { useToast } from '../../components/Toast'
import { useAsync } from '../../hooks/useAsync'
import { formatBytes } from '../../lib/units'

const GROUP_TONE: Record<string, 'ok' | 'muted'> = { native: 'ok', legacy_voucher: 'muted' }
const CARD_TONE: Record<Card['state'], 'ok' | 'muted' | 'warn' | 'danger'> = {
  unused: 'muted',
  active: 'ok',
  exhausted: 'warn',
  disabled: 'danger',
}

/** Card groups (v4-5, FR-136/137): list → create → manage (group actions,
 *  per-card list + reset/disable, print, usage export). */
export function CardsPage() {
  const t = useT()
  const { can } = useAuth()
  const groups = useAsync(() => listCardGroups(), [])
  const [createOpen, setCreateOpen] = useState(false)
  const [manageId, setManageId] = useState<string | null>(null)

  return (
    <section>
      <PageHeader
        title={t('cards.title')}
        actions={
          can(PERM_CARDS_CREATE) ? (
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              {t('cards.create')}
            </Button>
          ) : null
        }
      />

      {groups.error ? (
        <ErrorState onRetry={groups.reload} />
      ) : groups.loading || !groups.data ? (
        <LoadingState />
      ) : groups.data.items.length === 0 ? (
        <p className="rounded-md border border-dashed border-border p-8 text-center text-sm text-ink-muted">
          {t('cards.empty')}
        </p>
      ) : (
        <div className="overflow-x-auto rounded-md border border-border">
          <table className="w-full min-w-[48rem] text-sm">
            <thead className="bg-surface-sunken/40 text-xs text-ink-muted">
              <tr>
                <Th>{t('cards.col.name')}</Th>
                <Th className="text-end">{t('cards.col.total')}</Th>
                <Th className="text-end">{t('cards.col.unused')}</Th>
                <Th className="text-end">{t('cards.col.active')}</Th>
                <Th className="text-end">{t('cards.col.exhausted')}</Th>
                <Th>{t('cards.col.status')}</Th>
                <Th />
              </tr>
            </thead>
            <tbody>
              {groups.data.items.map((g) => (
                <tr key={g.id} className="border-t border-border/60">
                  <td className="px-3 py-2">
                    <button
                      type="button"
                      className="text-brand-strong hover:underline"
                      onClick={() => setManageId(g.id)}
                    >
                      {g.name}
                    </button>
                    {g.source === 'legacy_voucher' && (
                      <Pill tone={GROUP_TONE[g.source]} className="ms-2">
                        {t('cards.legacyVoucher')}
                      </Pill>
                    )}
                  </td>
                  <td className="px-3 py-2 text-end">{g.card_count}</td>
                  <td className="px-3 py-2 text-end">{g.unused_count}</td>
                  <td className="px-3 py-2 text-end">{g.active_count}</td>
                  <td className="px-3 py-2 text-end">{g.exhausted_count}</td>
                  <td className="px-3 py-2">
                    <Pill tone={g.enabled ? 'ok' : 'danger'}>
                      {g.enabled ? t('cards.enabled') : t('cards.disabled')}
                    </Pill>
                  </td>
                  <td className="px-3 py-2 text-end">
                    <Button size="sm" variant="ghost" onClick={() => setManageId(g.id)}>
                      {t('cards.manage')}
                    </Button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <CreateGroupModal open={createOpen} onOpenChange={setCreateOpen} onCreated={groups.reload} />
      <ManageGroupModal
        groupId={manageId}
        onClose={() => setManageId(null)}
        onChanged={groups.reload}
      />
    </section>
  )
}

function Th({ children, className = '' }: { children?: React.ReactNode; className?: string }) {
  return <th className={`px-3 py-2 text-start font-medium ${className}`}>{children}</th>
}

function CreateGroupModal({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean
  onOpenChange: (o: boolean) => void
  onCreated: () => void
}) {
  const t = useT()
  const [name, setName] = useState('')
  const [count, setCount] = useState(10)
  const [prefix, setPrefix] = useState('')
  const [codeFormat, setCodeFormat] = useState<CreateGroupInput['code_format']>('alphanumeric')
  const [codeLength, setCodeLength] = useState(10)
  const [price, setPrice] = useState(0)
  const [rateDown, setRateDown] = useState(0)
  const [rateUp, setRateUp] = useState(0)
  const [transferMode, setTransferMode] = useState<CreateGroupInput['transfer_mode']>('unlimited')
  const [transferTotalMB, setTransferTotalMB] = useState(1024)
  const [uptimeHours, setUptimeHours] = useState(0)
  const [expireAfterHours, setExpireAfterHours] = useState(0)
  const [validUntil, setValidUntil] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [generated, setGenerated] = useState<{ groupId: string; codes: string[] } | null>(null)

  async function submit() {
    setBusy(true)
    setError(null)
    try {
      const input: CreateGroupInput = {
        name,
        count,
        username_prefix: prefix.trim() || undefined,
        code_format: codeFormat,
        code_length: codeLength,
        price,
        rate_down_kbps: rateDown,
        rate_up_kbps: rateUp,
        transfer_mode: transferMode,
        transfer_total_bytes:
          transferMode === 'combined' ? transferTotalMB * 1024 * 1024 : undefined,
        uptime_limit_seconds: uptimeHours > 0 ? uptimeHours * 3600 : undefined,
        expire_after_seconds: expireAfterHours > 0 ? expireAfterHours * 3600 : undefined,
        valid_until: validUntil ? new Date(validUntil).toISOString() : undefined,
      }
      const { group, codes } = await createCardGroup(input)
      setGenerated({ groupId: group.id, codes })
      onCreated()
    } catch (err) {
      setError(err instanceof Error ? err.message : t('common.error.body'))
    } finally {
      setBusy(false)
    }
  }

  function close() {
    setGenerated(null)
    setName('')
    onOpenChange(false)
  }

  return (
    <Modal
      open={open}
      onOpenChange={busy ? () => {} : close}
      title={t('cards.createTitle')}
      size="lg"
    >
      {generated ? (
        <GeneratedCodesView groupId={generated.groupId} codes={generated.codes} onDone={close} />
      ) : (
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void submit()
          }}
          className="space-y-4"
        >
          <Field label={t('cards.name')} htmlFor="cg-name">
            <TextInput
              id="cg-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              required
            />
          </Field>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t('cards.count')} htmlFor="cg-count">
              <TextInput
                id="cg-count"
                type="number"
                min={1}
                max={10000}
                value={count}
                onChange={(e) => setCount(Number(e.target.value))}
                dir="ltr"
              />
            </Field>
            <Field label={t('cards.prefix')} htmlFor="cg-prefix">
              <TextInput
                id="cg-prefix"
                value={prefix}
                onChange={(e) => setPrefix(e.target.value)}
                dir="ltr"
              />
            </Field>
          </div>
          <div className="grid grid-cols-2 gap-3">
            <Field label={t('cards.codeFormat')} htmlFor="cg-format">
              <Select
                id="cg-format"
                value={codeFormat}
                onChange={(e) => setCodeFormat(e.target.value as CreateGroupInput['code_format'])}
              >
                <option value="alphanumeric">{t('cards.format.alphanumeric')}</option>
                <option value="alpha">{t('cards.format.alpha')}</option>
                <option value="numeric">{t('cards.format.numeric')}</option>
              </Select>
            </Field>
            <Field label={t('cards.codeLength')} htmlFor="cg-length">
              <TextInput
                id="cg-length"
                type="number"
                min={6}
                max={24}
                value={codeLength}
                onChange={(e) => setCodeLength(Number(e.target.value))}
                dir="ltr"
              />
            </Field>
          </div>
          <div className="grid grid-cols-3 gap-3">
            <Field label={t('cards.price')} htmlFor="cg-price">
              <TextInput
                id="cg-price"
                type="number"
                min={0}
                value={price}
                onChange={(e) => setPrice(Number(e.target.value))}
                dir="ltr"
              />
            </Field>
            <Field label={t('cards.rateDown')} htmlFor="cg-down">
              <TextInput
                id="cg-down"
                type="number"
                min={0}
                value={rateDown}
                onChange={(e) => setRateDown(Number(e.target.value))}
                dir="ltr"
              />
            </Field>
            <Field label={t('cards.rateUp')} htmlFor="cg-up">
              <TextInput
                id="cg-up"
                type="number"
                min={0}
                value={rateUp}
                onChange={(e) => setRateUp(Number(e.target.value))}
                dir="ltr"
              />
            </Field>
          </div>
          <Field label={t('cards.transferMode')} htmlFor="cg-transfer">
            <Select
              id="cg-transfer"
              value={transferMode}
              onChange={(e) => setTransferMode(e.target.value as CreateGroupInput['transfer_mode'])}
            >
              <option value="unlimited">{t('cards.transfer.unlimited')}</option>
              <option value="combined">{t('cards.transfer.combined')}</option>
            </Select>
          </Field>
          {transferMode === 'combined' && (
            <Field label={t('cards.transferTotalMB')} htmlFor="cg-total">
              <TextInput
                id="cg-total"
                type="number"
                min={1}
                value={transferTotalMB}
                onChange={(e) => setTransferTotalMB(Number(e.target.value))}
                dir="ltr"
              />
            </Field>
          )}
          <div className="grid grid-cols-2 gap-3">
            <Field
              label={t('cards.uptimeHours')}
              hint={t('cards.uptimeHoursHint')}
              htmlFor="cg-uptime"
            >
              <TextInput
                id="cg-uptime"
                type="number"
                min={0}
                value={uptimeHours}
                onChange={(e) => setUptimeHours(Number(e.target.value))}
                dir="ltr"
              />
            </Field>
            <Field
              label={t('cards.expireAfterHours')}
              hint={t('cards.expireAfterHoursHint')}
              htmlFor="cg-expire"
            >
              <TextInput
                id="cg-expire"
                type="number"
                min={0}
                value={expireAfterHours}
                onChange={(e) => setExpireAfterHours(Number(e.target.value))}
                dir="ltr"
              />
            </Field>
          </div>
          <Field label={t('cards.validUntil')} hint={t('cards.validUntilHint')} htmlFor="cg-valid">
            <TextInput
              id="cg-valid"
              type="date"
              value={validUntil}
              onChange={(e) => setValidUntil(e.target.value)}
            />
          </Field>
          {error ? <p className="text-sm text-danger">{error}</p> : null}
          <div className="flex justify-end gap-2">
            <Button variant="ghost" disabled={busy} onClick={close}>
              {t('ui.cancel')}
            </Button>
            <Button type="submit" disabled={busy || !name || count < 1}>
              {busy ? t('ui.working') : t('cards.generate')}
            </Button>
          </div>
        </form>
      )}
    </Modal>
  )
}

/** Shown once, immediately after generation — the only time plaintext codes
 *  exist (mirrors vouchers' generation-time CSV). */
function GeneratedCodesView({
  groupId,
  codes,
  onDone,
}: {
  groupId: string
  codes: string[]
  onDone: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const { can } = useAuth()

  async function print() {
    try {
      const html = await printCardGroup(groupId, codes)
      const w = window.open('', '_blank')
      if (w) {
        w.document.write(html)
        w.document.close()
      }
    } catch {
      toast(t('cards.printFailed'), 'danger')
    }
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-ok">{t('cards.generatedCount', { n: codes.length })}</p>
      <p className="text-xs text-ink-muted">{t('cards.codesOnceNote')}</p>
      <ul className="max-h-72 space-y-1 overflow-y-auto rounded-md border border-border p-2 font-mono text-sm">
        {codes.map((c) => (
          <li key={c}>{c}</li>
        ))}
      </ul>
      <div className="flex justify-end gap-2">
        {can(PERM_CARDS_PRINT) && (
          <Button variant="secondary" onClick={() => void print()}>
            {t('cards.print')}
          </Button>
        )}
        <Button onClick={onDone}>{t('ui.done')}</Button>
      </div>
    </div>
  )
}

function ManageGroupModal({
  groupId,
  onClose,
  onChanged,
}: {
  groupId: string | null
  onClose: () => void
  onChanged: () => void
}) {
  const t = useT()
  const { formatDate } = useFormatters()
  const { can } = useAuth()
  const { toast } = useToast()
  const groupQ = useAsync<CardGroup | null>(
    () => (groupId ? getCardGroup(groupId) : Promise.resolve(null)),
    [groupId],
  )
  const cardsQ = useAsync<{ items: Card[] } | null>(
    () => (groupId ? listCards(groupId) : Promise.resolve(null)),
    [groupId],
  )
  const [addCount, setAddCount] = useState(10)
  const [extendDate, setExtendDate] = useState('')
  const [newCodes, setNewCodes] = useState<string[] | null>(null)
  const [confirmDisable, setConfirmDisable] = useState(false)

  const g = groupQ.data

  function refresh() {
    groupQ.reload()
    cardsQ.reload()
    onChanged()
  }

  async function toggleGroup() {
    if (!g) return
    await setGroupEnabled(g.id, !g.enabled)
    refresh()
  }

  async function doExtend() {
    if (!g || !extendDate) return
    await extendGroup(g.id, new Date(extendDate).toISOString())
    setExtendDate('')
    refresh()
    toast(t('cards.extended'), 'ok')
  }

  async function doAddMore() {
    if (!g) return
    const res = await addMoreCards(g.id, addCount)
    setNewCodes(res.codes)
    refresh()
  }

  async function doDuplicate() {
    if (!g) return
    const res = await duplicateGroup(g.id, addCount)
    toast(t('cards.duplicated', { name: res.group.name }), 'ok')
    refresh()
  }

  async function doResetCard(id: string) {
    await resetCard(id)
    cardsQ.reload()
  }

  async function doToggleCard(c: Card) {
    await setCardEnabled(c.id, c.state === 'disabled')
    cardsQ.reload()
  }

  return (
    <Modal
      open={groupId !== null}
      onOpenChange={(o) => !o && onClose()}
      title={g?.name ?? ''}
      size="lg"
    >
      {groupQ.error || cardsQ.error ? (
        <ErrorState onRetry={refresh} />
      ) : groupQ.loading || !g ? (
        <LoadingState />
      ) : (
        <div className="space-y-5">
          <div className="flex flex-wrap gap-2">
            {can(PERM_CARDS_EDIT) && (
              <Button
                size="sm"
                variant="secondary"
                onClick={() => (g.enabled ? setConfirmDisable(true) : void toggleGroup())}
              >
                {g.enabled ? t('cards.disableAll') : t('cards.enableAll')}
              </Button>
            )}
            <a href={`${API_BASE}${cardUsageCSVURL(g.id)}`} target="_blank" rel="noreferrer">
              <Button size="sm" variant="ghost">
                {t('cards.exportUsage')}
              </Button>
            </a>
          </div>

          {can(PERM_CARDS_EDIT) && (
            <div className="grid grid-cols-2 gap-3 rounded-md border border-border p-3 text-sm sm:grid-cols-4">
              <div>
                <label className="mb-1 block text-xs text-ink-muted" htmlFor="mg-extend">
                  {t('cards.validUntil')}
                </label>
                <div className="flex gap-1">
                  <TextInput
                    id="mg-extend"
                    type="date"
                    value={extendDate}
                    onChange={(e) => setExtendDate(e.target.value)}
                  />
                  <Button size="sm" disabled={!extendDate} onClick={() => void doExtend()}>
                    {t('cards.extend')}
                  </Button>
                </div>
              </div>
              <div>
                <label className="mb-1 block text-xs text-ink-muted" htmlFor="mg-count">
                  {t('cards.count')}
                </label>
                <TextInput
                  id="mg-count"
                  type="number"
                  min={1}
                  value={addCount}
                  onChange={(e) => setAddCount(Number(e.target.value))}
                  dir="ltr"
                />
              </div>
              <div className="flex items-end">
                <Button size="sm" variant="secondary" onClick={() => void doAddMore()}>
                  {t('cards.addMore')}
                </Button>
              </div>
              <div className="flex items-end">
                <Button size="sm" variant="secondary" onClick={() => void doDuplicate()}>
                  {t('cards.duplicate')}
                </Button>
              </div>
            </div>
          )}

          {newCodes && (
            <div className="rounded-md border border-ok/30 bg-ok/5 p-3">
              <GeneratedCodesView
                groupId={g.id}
                codes={newCodes}
                onDone={() => setNewCodes(null)}
              />
            </div>
          )}

          <div className="max-h-72 overflow-auto rounded-md border border-border">
            <table className="w-full text-sm">
              <thead className="sticky top-0 bg-surface-sunken/60 text-xs text-ink-muted">
                <tr>
                  <Th>{t('cards.col.id')}</Th>
                  <Th>{t('cards.col.status')}</Th>
                  <Th>{t('cards.firstLogin')}</Th>
                  <Th className="text-end">{t('cards.usage')}</Th>
                  <Th />
                </tr>
              </thead>
              <tbody>
                {(cardsQ.data?.items ?? []).map((c) => (
                  <tr key={c.id} className="border-t border-border/60">
                    <td className="px-3 py-2">
                      <code>{c.id.slice(0, 8)}</code>
                    </td>
                    <td className="px-3 py-2">
                      <Pill tone={CARD_TONE[c.state]}>{t(`cards.state.${c.state}`)}</Pill>
                    </td>
                    <td className="px-3 py-2">
                      {c.first_login_at
                        ? formatDate(c.first_login_at, { timeStyle: undefined })
                        : '—'}
                    </td>
                    <td className="px-3 py-2 text-end">
                      <CardUsageCell card={c} />
                    </td>
                    <td className="px-3 py-2 text-end">
                      {can(PERM_CARDS_EDIT) && (
                        <div className="flex justify-end gap-1">
                          <Button size="sm" variant="ghost" onClick={() => void doResetCard(c.id)}>
                            {t('cards.resetCard')}
                          </Button>
                          <Button size="sm" variant="ghost" onClick={() => void doToggleCard(c)}>
                            {c.state === 'disabled' ? t('cards.enableAll') : t('cards.disableAll')}
                          </Button>
                        </div>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      <ConfirmDialog
        open={confirmDisable}
        onOpenChange={setConfirmDisable}
        title={t('cards.disableAllTitle')}
        body={t('cards.disableAllBody')}
        confirmLabel={t('cards.disableAll')}
        destructive
        onConfirm={toggleGroup}
      />
    </Modal>
  )
}

function CardUsageCell({ card }: { card: Card }) {
  const { formatNumber } = useFormatters()
  return (
    <span className="text-xs text-ink-muted">
      {formatBytes(card.usage_down_bytes + card.usage_up_bytes, formatNumber)}
    </span>
  )
}

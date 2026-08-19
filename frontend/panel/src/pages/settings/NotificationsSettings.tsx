import { useState } from 'react'

import { LoadingState, useT } from '@hikrad/shared'

import { testNotification, type NotificationChannel } from '../../api/setup'
import { useAuth } from '../../auth/AuthContext'
import { PERM_SETTINGS_EDIT } from '../../auth/permissions'
import { Button } from '../../components/Button'
import { TextInput } from '../../components/form'
import { FormField } from '../../components/patterns'
import { useToast } from '../../components/Toast'
import { useSettingsGroup } from './useSettingsGroup'

interface Smtp {
  host?: string
  port?: number
  username?: string
  password?: string
  from?: string
}
interface Telegram {
  bot_token?: string
}
interface WhatsApp {
  access_token?: string
  phone_number_id?: string
}
interface Sms {
  provider?: string
  url?: string
  method?: string
  api_key?: string
}

/** Notifications settings (FR-53.2): SMTP/Telegram/WhatsApp creds + send-test. */
export function NotificationsSettings() {
  const t = useT()
  const { can } = useAuth()
  const canEdit = can(PERM_SETTINGS_EDIT)
  const g = useSettingsGroup('notifications')

  if (!g.loaded) return <LoadingState />

  const smtp = (g.values.smtp ?? {}) as Smtp
  const telegram = (g.values.telegram ?? {}) as Telegram
  const whatsapp = (g.values.whatsapp ?? {}) as WhatsApp
  const sms = (g.values.sms ?? {}) as Sms

  async function submit() {
    await g.save(
      { smtp, telegram, whatsapp, sms: { ...sms, method: sms.method || 'POST' } },
      t('settings.saved'),
      t('common.error.body'),
    )
  }

  return (
    <div className="max-w-lg space-y-8">
      <section>
        <h2 className="mb-3 text-sm font-semibold">{t('settings.notifications.email')}</h2>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label={t('settings.notifications.smtpHost')}>
            {(id) => (
              <TextInput
                id={id}
                dir="ltr"
                disabled={!canEdit}
                value={smtp.host ?? ''}
                onChange={(e) => g.setField('smtp', { ...smtp, host: e.target.value })}
              />
            )}
          </FormField>
          <FormField label={t('settings.notifications.smtpPort')}>
            {(id) => (
              <TextInput
                id={id}
                type="number"
                dir="ltr"
                disabled={!canEdit}
                value={smtp.port ?? ''}
                onChange={(e) => g.setField('smtp', { ...smtp, port: Number(e.target.value) })}
              />
            )}
          </FormField>
          <FormField label={t('settings.notifications.smtpUsername')}>
            {(id) => (
              <TextInput
                id={id}
                dir="ltr"
                disabled={!canEdit}
                value={smtp.username ?? ''}
                onChange={(e) => g.setField('smtp', { ...smtp, username: e.target.value })}
              />
            )}
          </FormField>
          <FormField label={t('settings.notifications.smtpPassword')}>
            {(id) => (
              <TextInput
                id={id}
                type="password"
                dir="ltr"
                disabled={!canEdit}
                value={smtp.password ?? ''}
                onChange={(e) => g.setField('smtp', { ...smtp, password: e.target.value })}
              />
            )}
          </FormField>
          <FormField label={t('settings.notifications.smtpFrom')}>
            {(id) => (
              <TextInput
                id={id}
                dir="ltr"
                disabled={!canEdit}
                value={smtp.from ?? ''}
                onChange={(e) => g.setField('smtp', { ...smtp, from: e.target.value })}
              />
            )}
          </FormField>
        </div>
        <TestSendRow channel="email" />
      </section>

      <section>
        <h2 className="mb-3 text-sm font-semibold">{t('settings.notifications.telegram')}</h2>
        <FormField label={t('settings.notifications.botToken')}>
          {(id) => (
            <TextInput
              id={id}
              dir="ltr"
              disabled={!canEdit}
              value={telegram.bot_token ?? ''}
              onChange={(e) => g.setField('telegram', { ...telegram, bot_token: e.target.value })}
            />
          )}
        </FormField>
        <TestSendRow channel="telegram" />
      </section>

      <section>
        <h2 className="mb-3 text-sm font-semibold">{t('settings.notifications.whatsapp')}</h2>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label={t('settings.notifications.accessToken')}>
            {(id) => (
              <TextInput
                id={id}
                dir="ltr"
                disabled={!canEdit}
                value={whatsapp.access_token ?? ''}
                onChange={(e) =>
                  g.setField('whatsapp', { ...whatsapp, access_token: e.target.value })
                }
              />
            )}
          </FormField>
          <FormField label={t('settings.notifications.phoneNumberId')}>
            {(id) => (
              <TextInput
                id={id}
                dir="ltr"
                disabled={!canEdit}
                value={whatsapp.phone_number_id ?? ''}
                onChange={(e) =>
                  g.setField('whatsapp', { ...whatsapp, phone_number_id: e.target.value })
                }
              />
            )}
          </FormField>
        </div>
        <TestSendRow channel="whatsapp" />
      </section>

      <section>
        <h2 className="mb-3 text-sm font-semibold">{t('settings.notifications.sms')}</h2>
        <p className="mb-3 text-xs text-ink-muted">{t('settings.notifications.smsHint')}</p>
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label={t('settings.notifications.smsUrl')}>
            {(id) => (
              <TextInput
                id={id}
                dir="ltr"
                disabled={!canEdit}
                value={sms.url ?? ''}
                onChange={(e) => g.setField('sms', { ...sms, url: e.target.value })}
              />
            )}
          </FormField>
          <FormField label={t('settings.notifications.smsMethod')}>
            {(id) => (
              <TextInput
                id={id}
                dir="ltr"
                disabled={!canEdit}
                value={sms.method ?? 'POST'}
                onChange={(e) => g.setField('sms', { ...sms, method: e.target.value })}
              />
            )}
          </FormField>
          <FormField label={t('settings.notifications.smsApiKey')}>
            {(id) => (
              <TextInput
                id={id}
                type="password"
                dir="ltr"
                disabled={!canEdit}
                value={sms.api_key ?? ''}
                onChange={(e) => g.setField('sms', { ...sms, api_key: e.target.value })}
              />
            )}
          </FormField>
        </div>
        <TestSendRow channel="sms" />
      </section>

      {canEdit ? (
        <Button disabled={g.saving} onClick={() => void submit()}>
          {g.saving ? t('ui.working') : t('ui.save')}
        </Button>
      ) : null}
    </div>
  )
}

function TestSendRow({ channel }: { channel: NotificationChannel }) {
  const t = useT()
  const { toast } = useToast()
  const [recipient, setRecipient] = useState('')
  const [busy, setBusy] = useState(false)

  async function send() {
    if (!recipient.trim()) return
    setBusy(true)
    try {
      const res = await testNotification(channel, recipient.trim())
      toast(
        res.ok
          ? t('settings.notifications.testOk')
          : (res.error ?? t('settings.notifications.testFailed')),
        res.ok ? 'ok' : 'danger',
      )
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="mt-2 flex items-center gap-2">
      <TextInput
        dir="ltr"
        placeholder={t('settings.notifications.testRecipient')}
        value={recipient}
        onChange={(e) => setRecipient(e.target.value)}
        className="max-w-xs"
      />
      <Button size="sm" variant="secondary" disabled={busy} onClick={() => void send()}>
        {busy ? t('ui.working') : t('settings.notifications.sendTest')}
      </Button>
    </div>
  )
}

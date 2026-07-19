import { useRef, useState } from 'react'
import { useNavigate } from 'react-router-dom'

import { parseRateKbps, useT } from '@hikrad/shared'

import { createProfile } from '../api/profiles'
import type { ProfileWrite } from '../api/types'
import {
  createSetupAdmin,
  getSetupLicense,
  uploadSetupLicense,
  type LicenseResponse,
} from '../api/setup'
import { ApiError } from '../api/client'
import { useAuth } from '../auth/AuthContext'
import { Button } from '../components/Button'
import { CopyButton } from '../components/CopyButton'
import { Field, TextInput, Textarea } from '../components/form'
import { useToast } from '../components/Toast'
import { useAsync } from '../hooks/useAsync'
import { NasWizardModal } from '../pages/nas/NasWizardModal'
import { BrandingSettings } from '../pages/settings/BrandingSettings'

type Step = 'license' | 'admin' | 'branding' | 'nas' | 'profile' | 'done'
const STEP_ORDER: Step[] = ['license', 'admin', 'branding', 'nas', 'profile', 'done']

/**
 * First-run wizard (FR-49.3, contract C4): license → admin → branding → first
 * NAS + profile (both optional) → dashboard. License/admin run unauthenticated
 * against /api/v1/setup/*; once the admin exists, branding/NAS/profile reuse
 * the normal authenticated endpoints (setup's own branding route refuses the
 * moment an admin exists — see setupapi/wizard_api.go). Resumable: the step
 * is persisted so a reload continues where it left off.
 */
export function SetupWizardPage({ onSetupComplete }: { onSetupComplete: () => void }) {
  const t = useT()
  const { login } = useAuth()
  const navigate = useNavigate()
  const [step, setStep] = useState<Step>(
    () => (localStorage.getItem('hikrad:setup:step') as Step) || 'license',
  )

  function go(next: Step) {
    setStep(next)
    localStorage.setItem('hikrad:setup:step', next)
  }

  function finish() {
    localStorage.removeItem('hikrad:setup:step')
    // SetupGate sits above the router and only checks admin_exists once on
    // mount — a plain navigate('/') doesn't touch that cached state, so the
    // gate kept rendering this same wizard instance after "Go to dashboard"
    // (only a full page reload happened to force a re-check). Explicitly
    // invalidating the gate's check is the real fix, not the reload.
    onSetupComplete()
    navigate('/')
  }

  return (
    <div className="mx-auto min-h-screen max-w-lg px-4 py-10">
      <h1 className="mb-1 text-2xl font-bold text-brand">{t('common.productName')}</h1>
      <p className="mb-6 text-sm text-ink-muted">{t('setup.subtitle')}</p>
      <StepDots step={step} />

      {step === 'license' && <LicenseStep onNext={() => go('admin')} />}
      {step === 'admin' && (
        <AdminStep
          onDone={async (username, password) => {
            await login(username, password)
            go('branding')
          }}
        />
      )}
      {step === 'branding' && (
        <div>
          <BrandingSettings />
          <div className="mt-4">
            <Button onClick={() => go('nas')}>{t('ui.next')}</Button>
          </div>
        </div>
      )}
      {step === 'nas' && <NasStep onNext={() => go('profile')} onSkip={() => go('profile')} />}
      {step === 'profile' && <ProfileStep onNext={() => go('done')} onSkip={() => go('done')} />}
      {step === 'done' && <DoneStep onFinish={finish} />}
    </div>
  )
}

function StepDots({ step }: { step: Step }) {
  const idx = STEP_ORDER.indexOf(step)
  return (
    <div className="mb-6 flex gap-1.5">
      {STEP_ORDER.map((s, i) => (
        <span
          key={s}
          className={`h-1.5 flex-1 rounded-full ${i <= idx ? 'bg-brand' : 'bg-surface-sunken'}`}
        />
      ))}
    </div>
  )
}

function LicenseStep({ onNext }: { onNext: () => void }) {
  const t = useT()
  const { toast } = useToast()
  const { data } = useAsync(() => getSetupLicense(), [])
  const [payload, setPayload] = useState('')
  const [signature, setSignature] = useState('')
  const [busy, setBusy] = useState(false)
  const fileRef = useRef<HTMLInputElement>(null)

  async function submit() {
    setBusy(true)
    try {
      const parsed = JSON.parse(payload) as unknown
      await uploadSetupLicense(parsed, signature.trim())
      toast(t('setup.license.installed'), 'ok')
      onNext()
    } catch (err) {
      toast(err instanceof ApiError ? err.message : t('setup.license.invalidJson'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  async function onFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0]
    if (!file) return
    const text = await file.text()
    try {
      const parsed = JSON.parse(text) as { payload?: unknown; signature?: string }
      if (parsed.payload && parsed.signature) {
        setPayload(JSON.stringify(parsed.payload))
        setSignature(parsed.signature)
      }
    } catch {
      toast(t('setup.license.invalidJson'), 'danger')
    }
  }

  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold">{t('setup.step.license')}</h2>
      <p className="text-sm text-ink-muted">{t('setup.license.body')}</p>
      <Field label={t('setup.license.fingerprint')}>
        <div className="flex items-center gap-2">
          <code dir="ltr" className="flex-1 rounded-md bg-surface-sunken px-3 py-2 text-xs">
            {(data as LicenseResponse | undefined)?.fingerprint ?? '…'}
          </code>
          <CopyButton text={(data as LicenseResponse | undefined)?.fingerprint ?? ''} />
        </div>
      </Field>
      <div>
        <input ref={fileRef} type="file" accept=".json" onChange={(e) => void onFile(e)} />
      </div>
      <Field label={t('setup.license.payload')}>
        <Textarea rows={4} dir="ltr" value={payload} onChange={(e) => setPayload(e.target.value)} />
      </Field>
      <Field label={t('setup.license.signature')}>
        <TextInput dir="ltr" value={signature} onChange={(e) => setSignature(e.target.value)} />
      </Field>
      <p className="text-xs text-ink-muted">{t('setup.license.offlineNote')}</p>
      <div className="flex justify-between gap-2">
        <Button variant="ghost" onClick={onNext}>
          {t('setup.license.skip')}
        </Button>
        <Button disabled={busy || !payload || !signature} onClick={() => void submit()}>
          {busy ? t('ui.working') : t('ui.next')}
        </Button>
      </div>
    </div>
  )
}

function AdminStep({ onDone }: { onDone: (username: string, password: string) => Promise<void> }) {
  const t = useT()
  const { toast } = useToast()
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [confirm, setConfirm] = useState('')
  const [busy, setBusy] = useState(false)
  const [errors, setErrors] = useState<Record<string, string>>({})

  async function submit() {
    setErrors({})
    if (password !== confirm) {
      setErrors({ confirm: t('setup.admin.mismatch') })
      return
    }
    setBusy(true)
    try {
      await createSetupAdmin(username, password)
      await onDone(username, password)
    } catch (err) {
      if (err instanceof ApiError && err.fieldErrors.length > 0) {
        const next: Record<string, string> = {}
        for (const fe of err.fieldErrors) next[fe.field] = fe.message
        setErrors(next)
      } else {
        toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
      }
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold">{t('setup.step.admin')}</h2>
      <Field label={t('setup.admin.username')} error={errors.username}>
        <TextInput dir="ltr" value={username} onChange={(e) => setUsername(e.target.value)} />
      </Field>
      <Field label={t('setup.admin.password')} error={errors.password}>
        <TextInput
          type="password"
          dir="ltr"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
      </Field>
      <Field label={t('setup.admin.confirmPassword')} error={errors.confirm}>
        <TextInput
          type="password"
          dir="ltr"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
        />
      </Field>
      <Button disabled={busy || !username || password.length < 8} onClick={() => void submit()}>
        {busy ? t('ui.working') : t('ui.next')}
      </Button>
    </div>
  )
}

function NasStep({ onNext, onSkip }: { onNext: () => void; onSkip: () => void }) {
  const t = useT()
  const [open, setOpen] = useState(true)
  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold">{t('setup.step.nas')}</h2>
      <p className="text-sm text-ink-muted">{t('setup.nas.body')}</p>
      <Button variant="ghost" onClick={onSkip}>
        {t('setup.nas.skip')}
      </Button>
      <NasWizardModal
        open={open}
        onOpenChange={(o) => {
          setOpen(o)
          if (!o) onSkip()
        }}
        onSaved={() => {
          setOpen(false)
          onNext()
        }}
      />
    </div>
  )
}

function ProfileStep({ onNext, onSkip }: { onNext: () => void; onSkip: () => void }) {
  const t = useT()
  const { toast } = useToast()
  const [name, setName] = useState('')
  const [price, setPrice] = useState('10000')
  const [duration, setDuration] = useState('30')
  const [down, setDown] = useState('10M')
  const [up, setUp] = useState('10M')
  const [busy, setBusy] = useState(false)

  async function submit() {
    setBusy(true)
    try {
      const body: ProfileWrite = {
        name,
        price: Number(price),
        duration_days: Number(duration),
        rate_down_kbps: parseRateKbps(down) ?? 0,
        rate_up_kbps: parseRateKbps(up) ?? 0,
        session_limit_default: 1,
        quota_mode: 'unlimited',
        expiry_behavior: 'expired_pool',
        quota_behavior: 'block',
      }
      await createProfile(body)
      onNext()
    } catch (err) {
      toast(err instanceof Error ? err.message : t('common.error.body'), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold">{t('setup.step.profile')}</h2>
      <p className="text-sm text-ink-muted">{t('setup.profile.body')}</p>
      <Field label={t('setup.profile.name')}>
        <TextInput value={name} onChange={(e) => setName(e.target.value)} />
      </Field>
      <div className="grid grid-cols-3 gap-2">
        <Field label={t('setup.profile.priceIqd')}>
          <TextInput
            type="number"
            dir="ltr"
            value={price}
            onChange={(e) => setPrice(e.target.value)}
          />
        </Field>
        <Field label={t('setup.profile.durationDays')}>
          <TextInput
            type="number"
            dir="ltr"
            value={duration}
            onChange={(e) => setDuration(e.target.value)}
          />
        </Field>
        <Field label={t('setup.profile.rateKbps')} hint={t('rate.hint')}>
          <TextInput
            dir="ltr"
            value={down}
            onChange={(e) => {
              setDown(e.target.value)
              setUp(e.target.value)
            }}
          />
        </Field>
      </div>
      <div className="flex justify-between gap-2">
        <Button variant="ghost" onClick={onSkip}>
          {t('setup.profile.skip')}
        </Button>
        <Button disabled={busy || !name} onClick={() => void submit()}>
          {busy ? t('ui.working') : t('ui.next')}
        </Button>
      </div>
    </div>
  )
}

function DoneStep({ onFinish }: { onFinish: () => void }) {
  const t = useT()
  return (
    <div className="space-y-4">
      <h2 className="text-lg font-semibold">{t('setup.done.title')}</h2>
      <p className="text-sm text-ink-muted">{t('setup.done.body')}</p>
      <ul className="space-y-2 text-sm">
        <li>• {t('setup.done.whatNext.subscribers')}</li>
        <li>• {t('setup.done.whatNext.reports')}</li>
        <li>• {t('setup.done.whatNext.settings')}</li>
      </ul>
      <Button onClick={onFinish}>{t('setup.done.goToDashboard')}</Button>
    </div>
  )
}

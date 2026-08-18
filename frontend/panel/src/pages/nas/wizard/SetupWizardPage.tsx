import { useEffect, useMemo, useRef, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'

import { ErrorState, LoadingState, useT } from '@hikrad/shared'

import { getNas } from '../../../api/nas'
import { listPools } from '../../../api/pools'
import type {
  AutoSetupApplyResultItem,
  AutoSetupPreview,
  AutoSetupResolution,
  NasType,
  Pool,
  WizardDetection,
  WizardFormData,
  WizardIpv6Input,
  WizardServiceInput,
  WizardStepStatus,
  WizardWanInput,
} from '../../../api/types'
import { Button } from '../../../components/Button'
import { PageHeader } from '../../../components/PageHeader'
import { PlanApplyResults, PlanPreview } from '../../../components/PlanPreview'
import { Checkbox, Select, TextInput } from '../../../components/form'
import { FormField } from '../../../components/patterns'
import { useToast } from '../../../components/Toast'
import { useAsync } from '../../../hooks/useAsync'
import { routerErrorMessage } from '../../../lib/routerErrors'
import {
  applyWizard,
  detectWizard,
  getWizardState,
  planWizard,
  saveWizardState,
} from '../../../api/nasWizard'

const STEP_KEYS = [
  'detect',
  'bridge',
  'bridgePorts',
  'dns',
  'ntp',
  'wan',
  'radius',
  'services',
  'firewall',
  'review',
] as const

const TOTAL_STEPS = STEP_KEYS.length

const DNS_PRESETS: Record<string, string[]> = {
  google: ['8.8.8.8', '8.8.4.4'],
  cloudflare: ['1.1.1.1', '1.0.0.1'],
  quad9: ['9.9.9.9', '149.112.112.112'],
}

/**
 * FR-144 10-step setup wizard. The central design decision, load-bearing for
 * gate items 3/4: `formData` is the single accumulated object every step
 * reads and writes a slice of, and it is PUT to `/wizard/state` after every
 * transition — Next AND Back alike, always the WHOLE object — so going back
 * can never discard a later step's already-entered values, and closing the
 * browser mid-run resumes from exactly the saved step and data on reopen.
 *
 * `detect` (step 1) is read-only and only ever informs which later steps
 * default to "skip" — it never writes anything and is always safe to re-run.
 * `review` (step 10) is the only step that talks to the router beyond
 * reading: it calls `planWizard` for the literal command list (gate item 5)
 * and `applyWizard` behind the hash gate, exactly like every other write in
 * this codebase.
 */
export function SetupWizardPage() {
  const t = useT()
  const navigate = useNavigate()
  const { id = '' } = useParams()
  const { toast } = useToast()

  const nas = useAsync(() => getNas(id), [id])
  const stateQuery = useAsync(() => getWizardState(id), [id])
  const pools = useAsync(() => listPools(), [id])

  const [step, setStep] = useState(1)
  const [formData, setFormData] = useState<WizardFormData>({})
  const [resumed, setResumed] = useState(false)
  const [detection, setDetection] = useState<WizardDetection | null>(null)
  const savingRef = useRef(false)

  // Resume: once the saved state loads, jump straight to the saved step with
  // the saved data — a browser close mid-wizard is not destructive.
  useEffect(() => {
    if (resumed || !stateQuery.data) return
    setStep(stateQuery.data.completed ? 1 : Math.max(1, stateQuery.data.step))
    setFormData(stateQuery.data.form_data ?? {})
    setResumed(true)
  }, [stateQuery.data, resumed])

  async function persist(nextStep: number, data: WizardFormData) {
    savingRef.current = true
    try {
      await saveWizardState(id, nextStep, data)
    } catch {
      // Best-effort: a failed save must not block navigation — the operator
      // can still finish the run; resume simply won't have this step's data
      // if they close the browser before the next successful save.
    } finally {
      savingRef.current = false
    }
  }

  function goNext(patch?: Partial<WizardFormData>) {
    const next = patch ? { ...formData, ...patch } : formData
    setFormData(next)
    const nextStep = Math.min(TOTAL_STEPS, step + 1)
    setStep(nextStep)
    void persist(nextStep, next)
  }

  function goBack() {
    const prevStep = Math.max(1, step - 1)
    setStep(prevStep)
    void persist(prevStep, formData)
  }

  if (nas.error || stateQuery.error) return <ErrorState onRetry={() => window.location.reload()} />
  if (nas.loading || stateQuery.loading || !resumed) return <LoadingState />

  return (
    <section>
      <PageHeader
        title={t('nas.wizard.title', { name: nas.data?.name ?? '' })}
        subtitle={t('nas.wizard.stepOf', { step, total: TOTAL_STEPS })}
      />
      <WizardProgress step={step} />

      <div className="mt-4 max-w-2xl">
        {step === 1 ? (
          <DetectStep
            nasId={id}
            detection={detection}
            onDetected={setDetection}
            onNext={() => goNext()}
          />
        ) : null}
        {step === 2 ? (
          <BridgeNameStep
            status={detection?.bridge}
            formData={formData}
            onBack={goBack}
            onNext={(bridge) => goNext({ bridge })}
          />
        ) : null}
        {step === 3 ? (
          <BridgePortsStep
            formData={formData}
            onBack={goBack}
            onNext={(bridge) => goNext({ bridge })}
          />
        ) : null}
        {step === 4 ? (
          <DnsStep status={detection?.dns} onBack={goBack} onNext={(dns) => goNext({ dns })} />
        ) : null}
        {step === 5 ? (
          <NtpStep status={detection?.ntp} onBack={goBack} onNext={(ntp) => goNext({ ntp })} />
        ) : null}
        {step === 6 ? (
          <WanStep status={detection?.wan} onBack={goBack} onNext={(wan) => goNext({ wan })} />
        ) : null}
        {step === 7 ? (
          <RadiusStep
            status={detection?.radius}
            onBack={goBack}
            onNext={(values) => goNext({ values })}
          />
        ) : null}
        {step === 8 ? (
          <ServicesStep
            status={detection?.services}
            pools={pools.data?.items ?? []}
            onBack={goBack}
            onNext={(services, ipv6) => goNext({ services, ipv6 })}
          />
        ) : null}
        {step === 9 ? (
          <FirewallStep onBack={goBack} onNext={(firewall) => goNext({ firewall })} />
        ) : null}
        {step === 10 ? (
          <ReviewStep
            nasId={id}
            formData={formData}
            onBack={goBack}
            onDone={() => {
              toast(t('nas.wizard.completeToast'), 'ok')
              navigate(`/nas/${id}/device/overview`)
            }}
          />
        ) : null}
      </div>
    </section>
  )
}

function WizardProgress({ step }: { step: number }) {
  const t = useT()
  return (
    <ol className="flex flex-wrap gap-1.5">
      {STEP_KEYS.map((key, i) => {
        const n = i + 1
        const state = n < step ? 'done' : n === step ? 'current' : 'upcoming'
        return (
          <li
            key={key}
            className={`rounded-full px-2.5 py-1 text-xs ${
              state === 'current'
                ? 'bg-brand text-ink-inverse font-medium'
                : state === 'done'
                  ? 'bg-brand-soft text-brand-strong'
                  : 'bg-surface-sunken text-ink-muted'
            }`}
          >
            {n}. {t(`nas.wizard.step.${key}`)}
          </li>
        )
      })}
    </ol>
  )
}

function StepShell({
  title,
  hint,
  alreadyCorrect,
  children,
  onBack,
  onNext,
  nextDisabled,
  hideBack,
}: {
  title: string
  hint?: string
  alreadyCorrect?: WizardStepStatus
  children: React.ReactNode
  onBack?: () => void
  onNext: () => void
  nextDisabled?: boolean
  hideBack?: boolean
}) {
  const t = useT()
  return (
    <div className="space-y-4 rounded-md border border-border bg-surface-raised p-5">
      <div>
        <h2 className="text-base font-semibold">{title}</h2>
        {hint ? <p className="mt-1 text-sm text-ink-muted">{hint}</p> : null}
      </div>
      {alreadyCorrect?.already_correct ? (
        <p className="rounded-md bg-ok/10 p-2 text-sm text-ok">
          {t('nas.wizard.alreadyCorrect')} {alreadyCorrect.summary}
        </p>
      ) : null}
      {children}
      <div className="flex justify-between gap-2 pt-2">
        {!hideBack ? (
          <Button variant="ghost" onClick={onBack}>
            {t('ui.back')}
          </Button>
        ) : (
          <span />
        )}
        <Button disabled={nextDisabled} onClick={onNext}>
          {t('ui.next')}
        </Button>
      </div>
    </div>
  )
}

// --- Step 1: Detect ---------------------------------------------------------

function DetectStep({
  nasId,
  detection,
  onDetected,
  onNext,
}: {
  nasId: string
  detection: WizardDetection | null
  onDetected: (d: WizardDetection) => void
  onNext: () => void
}) {
  const t = useT()
  const [running, setRunning] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function run() {
    setRunning(true)
    setError(null)
    try {
      onDetected(await detectWizard(nasId))
    } catch (err) {
      setError(routerErrorMessage(err, t))
    } finally {
      setRunning(false)
    }
  }

  useEffect(() => {
    if (!detection) void run()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  return (
    <div className="space-y-4 rounded-md border border-border bg-surface-raised p-5">
      <div>
        <h2 className="text-base font-semibold">{t('nas.wizard.step.detect')}</h2>
        <p className="mt-1 text-sm text-ink-muted">{t('nas.wizard.detectHint')}</p>
      </div>
      {running ? <LoadingState /> : null}
      {error ? (
        <div>
          <p className="text-sm text-danger">{error}</p>
          <Button size="sm" variant="secondary" onClick={() => void run()}>
            {t('ui.retry')}
          </Button>
        </div>
      ) : null}
      {detection && !running ? (
        <ul className="space-y-1 text-sm">
          {(['bridge', 'dns', 'ntp', 'wan', 'radius', 'services'] as const).map((k) => (
            <li
              key={k}
              className="flex items-center justify-between gap-2 rounded bg-surface-sunken p-2"
            >
              <span>{t(`nas.wizard.step.${k === 'wan' ? 'wan' : k}`)}</span>
              <span className={detection[k].already_correct ? 'text-ok' : 'text-ink-muted'}>
                {detection[k].already_correct
                  ? t('nas.wizard.detectOk')
                  : t('nas.wizard.detectMissing')}
              </span>
            </li>
          ))}
        </ul>
      ) : null}
      <div className="flex justify-end pt-2">
        <Button disabled={running || !detection} onClick={onNext}>
          {t('nas.wizard.startButton')}
        </Button>
      </div>
    </div>
  )
}

// --- Step 2: Bridge name -----------------------------------------------------

function BridgeNameStep({
  status,
  formData,
  onBack,
  onNext,
}: {
  status?: WizardStepStatus
  formData: WizardFormData
  onBack: () => void
  onNext: (bridge: WizardFormData['bridge']) => void
}) {
  const t = useT()
  const [skip, setSkip] = useState(!!status?.already_correct)
  const [name, setName] = useState(formData.bridge?.name ?? 'bridge1')

  return (
    <StepShell
      title={t('nas.wizard.step.bridge')}
      hint={t('nas.wizard.bridgeHint')}
      alreadyCorrect={status}
      onBack={onBack}
      onNext={() => onNext(skip ? null : { name, ports: formData.bridge?.ports ?? [] })}
    >
      <Checkbox
        checked={skip}
        onChange={(e) => setSkip(e.target.checked)}
        label={t('nas.wizard.skipStep')}
      />
      {!skip ? (
        <FormField label={t('nas.wizard.bridgeName')}>
          {(id) => (
            <TextInput id={id} dir="ltr" value={name} onChange={(e) => setName(e.target.value)} />
          )}
        </FormField>
      ) : null}
    </StepShell>
  )
}

// --- Step 3: Bridge ports ----------------------------------------------------

function BridgePortsStep({
  formData,
  onBack,
  onNext,
}: {
  formData: WizardFormData
  onBack: () => void
  onNext: (bridge: WizardFormData['bridge']) => void
}) {
  const t = useT()
  const [ports, setPorts] = useState((formData.bridge?.ports ?? []).join(', '))

  if (!formData.bridge) {
    // The bridge step was skipped — there is no bridge to assign ports to.
    return (
      <StepShell
        title={t('nas.wizard.step.bridgePorts')}
        onBack={onBack}
        onNext={() => onNext(null)}
      >
        <p className="text-sm text-ink-muted">{t('nas.wizard.bridgePortsSkippedHint')}</p>
      </StepShell>
    )
  }

  return (
    <StepShell
      title={t('nas.wizard.step.bridgePorts')}
      hint={t('nas.wizard.bridgePortsHint')}
      onBack={onBack}
      onNext={() =>
        onNext({
          ...formData.bridge!,
          ports: ports
            .split(',')
            .map((p) => p.trim())
            .filter(Boolean),
        })
      }
    >
      <FormField label={t('nas.wizard.bridgePorts')} hint={t('nas.wizard.commaSeparatedHint')}>
        {(id) => (
          <TextInput id={id} dir="ltr" value={ports} onChange={(e) => setPorts(e.target.value)} />
        )}
      </FormField>
    </StepShell>
  )
}

// --- Step 4: DNS -------------------------------------------------------------

function DnsStep({
  status,
  onBack,
  onNext,
}: {
  status?: WizardStepStatus
  onBack: () => void
  onNext: (dns: WizardFormData['dns']) => void
}) {
  const t = useT()
  const [skip, setSkip] = useState(!!status?.already_correct)
  const [preset, setPreset] = useState('google')
  const [servers, setServers] = useState(DNS_PRESETS.google.join(', '))
  const [allowRemote, setAllowRemote] = useState(true)

  return (
    <StepShell
      title={t('nas.wizard.step.dns')}
      hint={t('nas.wizard.dnsHint')}
      alreadyCorrect={status}
      onBack={onBack}
      onNext={() =>
        onNext(
          skip
            ? null
            : {
                servers: servers
                  .split(',')
                  .map((s) => s.trim())
                  .filter(Boolean),
                allow_remote_requests: allowRemote,
                cache_size: 2048,
              },
        )
      }
    >
      <Checkbox
        checked={skip}
        onChange={(e) => setSkip(e.target.checked)}
        label={t('nas.wizard.skipStep')}
      />
      {!skip ? (
        <>
          <FormField label={t('nas.wizard.dnsPreset')}>
            {(id) => (
              <Select
                id={id}
                value={preset}
                onChange={(e) => {
                  setPreset(e.target.value)
                  if (DNS_PRESETS[e.target.value])
                    setServers(DNS_PRESETS[e.target.value].join(', '))
                }}
              >
                <option value="google">{t('nas.wizard.dnsProviderGoogle')}</option>
                <option value="cloudflare">{t('nas.wizard.dnsProviderCloudflare')}</option>
                <option value="quad9">{t('nas.wizard.dnsProviderQuad9')}</option>
                <option value="custom">{t('nas.wizard.dnsCustom')}</option>
              </Select>
            )}
          </FormField>
          <FormField label={t('nas.wizard.dnsServers')} hint={t('nas.wizard.commaSeparatedHint')}>
            {(id) => (
              <TextInput
                id={id}
                dir="ltr"
                value={servers}
                onChange={(e) => {
                  setPreset('custom')
                  setServers(e.target.value)
                }}
              />
            )}
          </FormField>
          <Checkbox
            checked={allowRemote}
            onChange={(e) => setAllowRemote(e.target.checked)}
            label={t('nas.wizard.dnsAllowRemote')}
          />
        </>
      ) : null}
    </StepShell>
  )
}

// --- Step 5: NTP -------------------------------------------------------------

function NtpStep({
  status,
  onBack,
  onNext,
}: {
  status?: WizardStepStatus
  onBack: () => void
  onNext: (ntp: WizardFormData['ntp']) => void
}) {
  const t = useT()
  const [skip, setSkip] = useState(!!status?.already_correct)
  const [servers, setServers] = useState('pool.ntp.org')

  return (
    <StepShell
      title={t('nas.wizard.step.ntp')}
      hint={t('nas.wizard.ntpHint')}
      alreadyCorrect={status}
      onBack={onBack}
      onNext={() =>
        onNext(
          skip
            ? null
            : {
                servers: servers
                  .split(',')
                  .map((s) => s.trim())
                  .filter(Boolean),
              },
        )
      }
    >
      <Checkbox
        checked={skip}
        onChange={(e) => setSkip(e.target.checked)}
        label={t('nas.wizard.skipStep')}
      />
      {!skip ? (
        <FormField label={t('nas.wizard.ntpServers')} hint={t('nas.wizard.commaSeparatedHint')}>
          {(id) => (
            <TextInput
              id={id}
              dir="ltr"
              value={servers}
              onChange={(e) => setServers(e.target.value)}
            />
          )}
        </FormField>
      ) : null}
    </StepShell>
  )
}

// --- Step 6: Internet / WAN --------------------------------------------------

function WanStep({
  status,
  onBack,
  onNext,
}: {
  status?: WizardStepStatus
  onBack: () => void
  onNext: (wan: WizardWanInput[]) => void
}) {
  const t = useT()
  const [skip, setSkip] = useState(!!status?.already_correct)
  const [iface, setIface] = useState('ether1')
  const [mode, setMode] = useState<'dhcp' | 'static' | 'pppoe'>('dhcp')

  return (
    <StepShell
      title={t('nas.wizard.step.wan')}
      hint={t('nas.wizard.wanHint')}
      alreadyCorrect={status}
      onBack={onBack}
      onNext={() => onNext(skip ? [] : [{ interface: iface, label: '', mode }])}
    >
      <Checkbox
        checked={skip}
        onChange={(e) => setSkip(e.target.checked)}
        label={t('nas.wizard.skipStep')}
      />
      {!skip ? (
        <div className="grid gap-3 sm:grid-cols-2">
          <FormField label={t('nas.internet.interface')}>
            {(id) => (
              <TextInput
                id={id}
                dir="ltr"
                value={iface}
                onChange={(e) => setIface(e.target.value)}
              />
            )}
          </FormField>
          <FormField label={t('nas.internet.modeLabel')}>
            {(id) => (
              <Select id={id} value={mode} onChange={(e) => setMode(e.target.value as typeof mode)}>
                <option value="dhcp">{t('nas.internet.mode.dhcp')}</option>
                <option value="static">{t('nas.internet.mode.static')}</option>
                <option value="pppoe">{t('nas.internet.mode.pppoe')}</option>
              </Select>
            )}
          </FormField>
        </div>
      ) : null}
    </StepShell>
  )
}

// --- Step 7: RADIUS to HikRAD ------------------------------------------------

function RadiusStep({
  status,
  onBack,
  onNext,
}: {
  status?: WizardStepStatus
  onBack: () => void
  onNext: (values: WizardFormData['values']) => void
}) {
  const t = useT()
  const [radiusServer, setRadiusServer] = useState('')
  const [coaPort, setCoaPort] = useState('3799')
  const [interim, setInterim] = useState('300')

  return (
    <StepShell
      title={t('nas.wizard.step.radius')}
      hint={t('nas.wizard.radiusHint')}
      alreadyCorrect={status}
      onBack={onBack}
      onNext={() =>
        onNext({
          radius_server: radiusServer || undefined,
          coa_port: coaPort ? Number(coaPort) : undefined,
          interim_secs: interim ? Number(interim) : undefined,
        })
      }
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <FormField label={t('nas.autoSetup.valuesRadiusServer')}>
          {(id) => (
            <TextInput
              id={id}
              dir="ltr"
              value={radiusServer}
              onChange={(e) => setRadiusServer(e.target.value)}
              placeholder={t('nas.wizard.radiusServerPlaceholder')}
            />
          )}
        </FormField>
        <FormField label={t('nas.autoSetup.valuesCoaPort')}>
          {(id) => (
            <TextInput
              id={id}
              dir="ltr"
              value={coaPort}
              onChange={(e) => setCoaPort(e.target.value)}
            />
          )}
        </FormField>
        <FormField label={t('nas.autoSetup.valuesInterimSecs')}>
          {(id) => (
            <TextInput
              id={id}
              dir="ltr"
              value={interim}
              onChange={(e) => setInterim(e.target.value)}
            />
          )}
        </FormField>
      </div>
    </StepShell>
  )
}

// --- Step 8: Services ---------------------------------------------------------

function ServicesStep({
  status,
  pools,
  onBack,
  onNext,
}: {
  status?: WizardStepStatus
  pools: Pool[]
  onBack: () => void
  onNext: (services: WizardServiceInput[], ipv6: WizardIpv6Input[]) => void
}) {
  const t = useT()
  const [kind, setKind] = useState<NasType>('pppoe')
  const [rosName, setRosName] = useState('hikrad-pppoe')
  const [iface, setIface] = useState('bridge1')
  const [poolId, setPoolId] = useState('')
  const [ipv6Enabled, setIpv6Enabled] = useState(false)
  const [ipv6Prefix, setIpv6Prefix] = useState('')
  const [ipv6Len, setIpv6Len] = useState('56')
  const [ipv6Pool, setIpv6Pool] = useState('hikrad-ipv6')

  return (
    <StepShell
      title={t('nas.wizard.step.services')}
      hint={t('nas.wizard.servicesHint')}
      alreadyCorrect={status}
      onBack={onBack}
      onNext={() => {
        const services: WizardServiceInput[] = [
          {
            service: kind,
            label: '',
            interface: iface,
            ros_server_name: rosName,
            ip_pool_id: poolId || null,
          },
        ]
        const ipv6: WizardIpv6Input[] = ipv6Enabled
          ? [
              {
                enabled: true,
                ros_server_name: rosName,
                prefix: ipv6Prefix,
                prefix_len: Number(ipv6Len) || 56,
                pool_name: ipv6Pool,
              },
            ]
          : []
        onNext(services, ipv6)
      }}
    >
      <div className="grid gap-3 sm:grid-cols-2">
        <FormField label={t('nas.serviceKind')}>
          {(id) => (
            <Select id={id} value={kind} onChange={(e) => setKind(e.target.value as NasType)}>
              <option value="pppoe">{t('nas.typeName.pppoe')}</option>
              <option value="hotspot">{t('nas.typeName.hotspot')}</option>
            </Select>
          )}
        </FormField>
        <FormField label={t('nas.rosServerName')}>
          {(id) => (
            <TextInput
              id={id}
              dir="ltr"
              value={rosName}
              onChange={(e) => setRosName(e.target.value)}
            />
          )}
        </FormField>
        <FormField label={t('nas.serverMgmt.interface')}>
          {(id) => (
            <TextInput id={id} dir="ltr" value={iface} onChange={(e) => setIface(e.target.value)} />
          )}
        </FormField>
        <FormField label={t('nas.serverMgmt.pool')}>
          {(id) => (
            <Select id={id} value={poolId} onChange={(e) => setPoolId(e.target.value)}>
              <option value="">{t('ui.none')}</option>
              {pools.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </Select>
          )}
        </FormField>
      </div>

      <div className="rounded-md border border-border p-3">
        <Checkbox
          checked={ipv6Enabled}
          onChange={(e) => setIpv6Enabled(e.target.checked)}
          label={t('nas.wizard.ipv6Enable')}
        />
        {ipv6Enabled ? (
          <div className="mt-3 grid gap-3 sm:grid-cols-3">
            <FormField label={t('nas.wizard.ipv6Prefix')}>
              {(id) => (
                <TextInput
                  id={id}
                  dir="ltr"
                  value={ipv6Prefix}
                  placeholder={t('nas.wizard.ipv6PrefixPlaceholder')}
                  onChange={(e) => setIpv6Prefix(e.target.value)}
                />
              )}
            </FormField>
            <FormField label={t('nas.wizard.ipv6PrefixLen')}>
              {(id) => (
                <TextInput
                  id={id}
                  dir="ltr"
                  value={ipv6Len}
                  onChange={(e) => setIpv6Len(e.target.value)}
                />
              )}
            </FormField>
            <FormField label={t('nas.wizard.ipv6PoolName')}>
              {(id) => (
                <TextInput
                  id={id}
                  dir="ltr"
                  value={ipv6Pool}
                  onChange={(e) => setIpv6Pool(e.target.value)}
                />
              )}
            </FormField>
          </div>
        ) : null}
      </div>
    </StepShell>
  )
}

// --- Step 9: Details & firewall ----------------------------------------------

function FirewallStep({
  onBack,
  onNext,
}: {
  onBack: () => void
  onNext: (firewall: WizardFormData['firewall']) => void
}) {
  const t = useT()
  const [natInterfaces, setNatInterfaces] = useState('ether1')
  const [expiredPoolEnabled, setExpiredPoolEnabled] = useState(false)
  const [poolName, setPoolName] = useState('expired-pool')
  const [ranges, setRanges] = useState('10.10.0.0/24')
  const [redirectUrl, setRedirectUrl] = useState('')

  return (
    <StepShell
      title={t('nas.wizard.step.firewall')}
      hint={t('nas.wizard.firewallStepHint')}
      onBack={onBack}
      onNext={() =>
        onNext({
          nat_out_interfaces: natInterfaces
            .split(',')
            .map((s) => s.trim())
            .filter(Boolean),
          expired_pool: expiredPoolEnabled
            ? {
                pool_name: poolName,
                ranges: ranges
                  .split(',')
                  .map((s) => s.trim())
                  .filter(Boolean),
                redirect_url: redirectUrl,
              }
            : null,
        })
      }
    >
      <FormField label={t('nas.wizard.natInterfaces')} hint={t('nas.wizard.commaSeparatedHint')}>
        {(id) => (
          <TextInput
            id={id}
            dir="ltr"
            value={natInterfaces}
            onChange={(e) => setNatInterfaces(e.target.value)}
          />
        )}
      </FormField>
      <div className="rounded-md border border-border p-3">
        <Checkbox
          checked={expiredPoolEnabled}
          onChange={(e) => setExpiredPoolEnabled(e.target.checked)}
          label={t('nas.wizard.expiredPoolEnable')}
        />
        {expiredPoolEnabled ? (
          <div className="mt-3 grid gap-3 sm:grid-cols-2">
            <FormField label={t('nas.wizard.expiredPoolName')}>
              {(id) => (
                <TextInput
                  id={id}
                  dir="ltr"
                  value={poolName}
                  onChange={(e) => setPoolName(e.target.value)}
                />
              )}
            </FormField>
            <FormField
              label={t('nas.wizard.expiredPoolRanges')}
              hint={t('nas.wizard.commaSeparatedHint')}
            >
              {(id) => (
                <TextInput
                  id={id}
                  dir="ltr"
                  value={ranges}
                  onChange={(e) => setRanges(e.target.value)}
                />
              )}
            </FormField>
            <FormField label={t('nas.wizard.expiredRedirectUrl')}>
              {(id) => (
                <TextInput
                  id={id}
                  dir="ltr"
                  value={redirectUrl}
                  onChange={(e) => setRedirectUrl(e.target.value)}
                />
              )}
            </FormField>
          </div>
        ) : null}
      </div>
    </StepShell>
  )
}

// --- Step 10: Review & push ---------------------------------------------------

function ReviewStep({
  nasId,
  formData,
  onBack,
  onDone,
}: {
  nasId: string
  formData: WizardFormData
  onBack: () => void
  onDone: () => void
}) {
  const t = useT()
  const { toast } = useToast()
  const [preview, setPreview] = useState<AutoSetupPreview | null>(null)
  const [resolutions, setResolutions] = useState<Record<string, AutoSetupResolution>>({})
  const [results, setResults] = useState<{
    items: AutoSetupApplyResultItem[]
    allOk: boolean
  } | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const body = useMemo(
    () => ({ ...formData, values: formData.values ?? {}, resolutions }),
    [formData, resolutions],
  )

  async function runPlan() {
    setBusy(true)
    setError(null)
    try {
      setPreview(await planWizard(nasId, body))
    } catch (err) {
      setError(routerErrorMessage(err, t))
    } finally {
      setBusy(false)
    }
  }

  useEffect(() => {
    void runPlan()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  async function runApply() {
    if (!preview) return
    setBusy(true)
    try {
      const res = await applyWizard(nasId, { ...body, preview_hash: preview.preview_hash })
      setResults({ items: res.results, allOk: res.all_ok })
      if (res.all_ok) {
        toast(t('nas.wizard.applyOk'), 'ok')
      } else {
        toast(t('nas.autoSetup.applyPartial'), 'danger')
      }
    } catch (err) {
      toast(routerErrorMessage(err, t), 'danger')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="space-y-4 rounded-md border border-border bg-surface-raised p-5">
      <div>
        <h2 className="text-base font-semibold">{t('nas.wizard.step.review')}</h2>
        <p className="mt-1 text-sm text-ink-muted">{t('nas.wizard.reviewHint')}</p>
      </div>

      {busy && !preview && !results ? <LoadingState /> : null}
      {error ? <p className="text-sm text-danger">{error}</p> : null}

      {preview && !results ? (
        <div className="space-y-3">
          <PlanPreview
            preview={preview}
            mode="resolve"
            resolutions={resolutions}
            onResolutionChange={(key, choice) =>
              setResolutions((prev) => {
                const next = { ...prev }
                if (choice) next[key] = choice
                else delete next[key]
                return next
              })
            }
          />
          {preview.conflicts.length > 0 ? (
            <Button size="sm" variant="secondary" disabled={busy} onClick={() => void runPlan()}>
              {t('nas.autoSetup.applyResolutions')}
            </Button>
          ) : null}
        </div>
      ) : null}

      {results ? (
        <>
          <PlanApplyResults results={results.items} allOk={results.allOk} />
          {results.allOk ? <Button onClick={onDone}>{t('nas.wizard.finishButton')}</Button> : null}
        </>
      ) : null}

      {!results ? (
        <div className="flex justify-between gap-2 pt-2">
          <Button variant="ghost" disabled={busy} onClick={onBack}>
            {t('ui.back')}
          </Button>
          {preview && preview.conflicts.length === 0 && preview.items.length > 0 ? (
            <Button disabled={busy} onClick={() => void runApply()}>
              {busy ? t('ui.working') : t('nas.wizard.pushButton')}
            </Button>
          ) : null}
          {preview && preview.items.length === 0 && preview.conflicts.length === 0 ? (
            <p className="text-sm text-ink-muted">{t('nas.plan.nothingToDo')}</p>
          ) : null}
        </div>
      ) : null}
    </div>
  )
}

import { useState } from 'react'

import {
  Card,
  DataTable,
  SectionCard,
  StatCard,
  StatusBadge,
  useT,
  type Column,
} from '@hikrad/shared'

import { LanguageSwitcher } from '../components/LanguageSwitcher'
import { ThemeToggle } from '../components/QuickToggles'
import { Button } from '../components/Button'
import { Field, Select, TextInput } from '../components/form'
import { DetailHeader } from '../components/patterns/DetailHeader'
import { FilterBar, type FilterField } from '../components/patterns/FilterBar'
import { FormField, FormSection } from '../components/patterns/FormSection'

/**
 * v3-1 FR-97.3 pattern gallery (dev-only, like /dev/rtl-smoke): every design
 * pattern rendered at once so it can be reviewed in both themes × en/ar/ku.
 * The "does not look AI-built" review surface (design-language.md).
 */
const filterFields: FilterField[] = [
  {
    key: 'status',
    label: 'Status',
    options: [
      { value: 'active', label: 'Active' },
      { value: 'expired', label: 'Expired' },
      { value: 'disabled', label: 'Disabled' },
    ],
  },
  {
    key: 'service',
    label: 'Service',
    options: [
      { value: 'pppoe', label: 'PPPoE' },
      { value: 'hotspot', label: 'Hotspot' },
    ],
  },
]

interface DemoRow {
  id: string
  name: string
  status: 'active' | 'expired' | 'disabled'
}
const demoRows: DemoRow[] = [
  { id: '1', name: 'ali.hassan', status: 'active' },
  { id: '2', name: 'sara.noor', status: 'expired' },
  { id: '3', name: 'omar.k', status: 'disabled' },
]

export function DesignGalleryPage() {
  const t = useT()
  const [search, setSearch] = useState('')
  const [filters, setFilters] = useState<Record<string, string>>({})

  const columns: Column<DemoRow>[] = [
    { key: 'name', header: t('subscriber.username'), cell: (r) => r.name },
    {
      key: 'status',
      header: t('subscriber.status'),
      cell: (r) => <StatusBadge status={r.status} />,
    },
    { key: 'id', header: 'ID', cell: (r) => r.id, align: 'end' },
  ]

  return (
    <section className="space-y-6">
      <DetailHeader
        title={t('design.title')}
        meta={t('design.subtitle')}
        actions={
          <>
            <LanguageSwitcher />
            <ThemeToggle />
          </>
        }
      />

      <SectionCard title={t('design.section.stats')}>
        <div className="grid gap-3 sm:grid-cols-3">
          <StatCard label={t('subscriber.status')} value="128" hint={t('design.demoHint')} />
          <StatCard
            label={t('design.section.stats')}
            value="42"
            delta={{ text: '+8%', dir: 'up' }}
          />
          {/* i18n-exempt: ARPU is a demo metric label in the dev-only gallery */}
          <StatCard label="ARPU" value="18,500" delta={{ text: '-3%', dir: 'down' }} />
        </div>
      </SectionCard>

      <SectionCard title={t('design.section.cards')}>
        <div className="grid gap-3 sm:grid-cols-2">
          <Card>{t('design.demoCardBody')}</Card>
          <Card>
            <div className="flex items-center justify-between gap-2">
              <span>{t('design.demoCardBody')}</span>
              <StatusBadge status="active" />
            </div>
          </Card>
        </div>
      </SectionCard>

      <SectionCard title={t('design.section.filter')}>
        <FilterBar
          search={search}
          onSearchChange={setSearch}
          fields={filterFields}
          values={filters}
          onChange={setFilters}
        />
        <DataTable
          columns={columns}
          rows={demoRows}
          getRowKey={(r) => r.id}
          minInlineSize="28rem"
        />
      </SectionCard>

      <SectionCard title={t('design.section.states')}>
        <div className="grid gap-3 sm:grid-cols-3">
          <div className="rounded-md border border-border">
            <DataTable columns={columns} rows={[]} getRowKey={(r) => r.id} loading />
          </div>
          <div className="rounded-md border border-border">
            <DataTable columns={columns} rows={[]} getRowKey={(r) => r.id} />
          </div>
          <div className="rounded-md border border-border">
            <DataTable
              columns={columns}
              rows={[]}
              getRowKey={(r) => r.id}
              error
              onRetry={() => {}}
            />
          </div>
        </div>
      </SectionCard>

      <SectionCard title={t('design.section.form')}>
        <div className="space-y-4">
          <FormSection title={t('design.form.identity')}>
            <FormField label={t('subscriber.username')}>
              {(id) => <TextInput id={id} dir="ltr" defaultValue="ali.hassan" />}
            </FormField>
            <FormField label={t('subscriber.name')}>
              {(id) => <TextInput id={id} defaultValue="Ali Hassan" />}
            </FormField>
          </FormSection>
          <FormSection title={t('design.form.service')}>
            <FormField label={t('subscriber.serviceType')}>
              {(id) => (
                <Select id={id} value="pppoe" onChange={() => {}}>
                  <option value="pppoe">{t('serviceType.pppoe')}</option>
                  <option value="hotspot">{t('serviceType.hotspot')}</option>
                </Select>
              )}
            </FormField>
          </FormSection>
          <FormSection title={t('design.form.advanced')} advanced>
            <Field label={t('design.demoHint')} htmlFor="design-adv">
              <TextInput id="design-adv" />
            </Field>
          </FormSection>
          <div className="flex justify-end gap-2">
            <Button variant="ghost">{t('ui.cancel')}</Button>
            <Button>{t('ui.save')}</Button>
          </div>
        </div>
      </SectionCard>
    </section>
  )
}

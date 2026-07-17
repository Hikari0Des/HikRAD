import type { ReactNode } from 'react'
import { Route, Routes } from 'react-router-dom'

import { RequireAuth } from './auth/AuthContext'
import { RequirePerm } from './auth/RequirePerm'
import {
  PERM_AUDIT_VIEW,
  PERM_PAYMENT_TICKETS_VERIFY,
  PERM_PAYMENT_PROVIDERS_MANAGE,
  PERM_LIVE_VIEW,
  PERM_MANAGERS_VIEW,
  PERM_MONITORING_VIEW,
  PERM_NAS_VIEW,
  PERM_POOLS_VIEW,
  PERM_PROFILES_VIEW,
  PERM_REPORTS_VIEW,
  PERM_SETTINGS_VIEW,
  PERM_OVERHEADS_MANAGE,
  PERM_SUBSCRIBERS_CREATE,
  PERM_SUBSCRIBERS_VIEW,
  PERM_TOPUP,
  PERM_VOUCHERS_VIEW,
} from './auth/permissions'
import { AppShell } from './shell/AppShell'
import { DashboardPage } from './pages/DashboardPage'
import { LoginPage } from './pages/LoginPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { RtlSmokePage } from './pages/RtlSmokePage'
import { CurrencyRatesPage } from './pages/billing/CurrencyRatesPage'
import { LedgerPage } from './pages/billing/LedgerPage'
import { MyPaymentMethodsPage } from './pages/billing/MyPaymentMethodsPage'
import { PaymentTicketsPage } from './pages/billing/PaymentTicketsPage'
import { PricingAdminPage } from './pages/billing/PricingAdminPage'
import { ProviderCatalogPage } from './pages/billing/ProviderCatalogPage'
import { VouchersPage } from './pages/billing/VouchersPage'
import { ImportWizardPage } from './pages/import/ImportWizardPage'
import { LicensePage } from './pages/license/LicensePage'
import { LiveSessionsPage } from './pages/live/LiveSessionsPage'
import { AlertsPage } from './pages/monitoring/AlertsPage'
import { DeviceStatusPage } from './pages/monitoring/DeviceStatusPage'
import { DevicesPage } from './pages/monitoring/DevicesPage'
import { HealthPage } from './pages/monitoring/HealthPage'
import { NasStatusPage } from './pages/monitoring/NasStatusPage'
import { NasListPage } from './pages/nas/NasListPage'
import { PoolsPage } from './pages/pools/PoolsPage'
import { ProfilesPage } from './pages/profiles/ProfilesPage'
import { DebugPage } from './pages/radius/DebugPage'
import { MarginReportPage } from './pages/reports/MarginReportPage'
import { RevenueReportPage } from './pages/reports/RevenueReportPage'
import { ReportsIndexPage } from './pages/reports/ReportsIndexPage'
import { SettlementReportPage } from './pages/reports/SettlementReportPage'
import { SubscribersReportPage } from './pages/reports/SubscribersReportPage'
import { UsageReportPage } from './pages/reports/UsageReportPage'
import { AccountSecurityPage } from './pages/security/AccountSecurityPage'
import { AuditLogPage } from './pages/security/AuditLogPage'
import { ManagersPage } from './pages/security/ManagersPage'
import { RolesPage } from './pages/security/RolesPage'
import { SettingsPage } from './pages/settings/SettingsPage'
import { UserDetailPage } from './pages/subscribers/UserDetailPage'
import { UserListPage } from './pages/subscribers/UserListPage'
import { SetupGate } from './setup/SetupGate'

/** Shorthand: mount a screen only when the manager holds the permission the
 * sidebar gates it on; otherwise render the friendly no-access state. */
function guard(perm: string, page: ReactNode): ReactNode {
  return <RequirePerm perm={perm}>{page}</RequirePerm>
}

export function App() {
  return (
    <SetupGate>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          element={
            <RequireAuth>
              <AppShell />
            </RequireAuth>
          }
        >
          <Route index element={<DashboardPage />} />
          <Route path="subscribers" element={guard(PERM_SUBSCRIBERS_VIEW, <UserListPage />)} />
          <Route
            path="subscribers/:id"
            element={guard(PERM_SUBSCRIBERS_VIEW, <UserDetailPage />)}
          />
          <Route path="profiles" element={guard(PERM_PROFILES_VIEW, <ProfilesPage />)} />
          <Route path="nas" element={guard(PERM_NAS_VIEW, <NasListPage />)} />
          <Route path="nas/:id/status" element={guard(PERM_NAS_VIEW, <NasStatusPage />)} />
          <Route path="pools" element={guard(PERM_POOLS_VIEW, <PoolsPage />)} />
          <Route path="sessions" element={guard(PERM_LIVE_VIEW, <LiveSessionsPage />)} />
          <Route path="ledger" element={guard(PERM_REPORTS_VIEW, <LedgerPage />)} />
          <Route path="vouchers" element={guard(PERM_VOUCHERS_VIEW, <VouchersPage />)} />
          <Route
            path="payment-tickets"
            element={guard(PERM_PAYMENT_TICKETS_VERIFY, <PaymentTicketsPage />)}
          />
          <Route
            path="payment-providers"
            element={guard(PERM_PAYMENT_PROVIDERS_MANAGE, <ProviderCatalogPage />)}
          />
          <Route path="my-payment-methods" element={<MyPaymentMethodsPage />} />
          <Route path="currency-rates" element={guard(PERM_TOPUP, <CurrencyRatesPage />)} />
          <Route
            path="pricing-admin"
            element={guard(PERM_OVERHEADS_MANAGE, <PricingAdminPage />)}
          />
          <Route path="reports" element={guard(PERM_REPORTS_VIEW, <ReportsIndexPage />)} />
          <Route path="reports/revenue" element={guard(PERM_REPORTS_VIEW, <RevenueReportPage />)} />
          <Route path="reports/margin" element={guard(PERM_REPORTS_VIEW, <MarginReportPage />)} />
          <Route
            path="reports/settlement"
            element={guard(PERM_REPORTS_VIEW, <SettlementReportPage />)}
          />
          <Route
            path="reports/subscribers"
            element={guard(PERM_REPORTS_VIEW, <SubscribersReportPage />)}
          />
          <Route path="reports/usage" element={guard(PERM_REPORTS_VIEW, <UsageReportPage />)} />
          <Route path="import" element={guard(PERM_SUBSCRIBERS_CREATE, <ImportWizardPage />)} />
          <Route path="devices" element={guard(PERM_MONITORING_VIEW, <DevicesPage />)} />
          <Route
            path="devices/:id/status"
            element={guard(PERM_MONITORING_VIEW, <DeviceStatusPage />)}
          />
          <Route path="health" element={guard(PERM_MONITORING_VIEW, <HealthPage />)} />
          <Route path="alerts" element={guard(PERM_MONITORING_VIEW, <AlertsPage />)} />
          <Route path="debug" element={guard(PERM_NAS_VIEW, <DebugPage />)} />
          <Route path="managers" element={guard(PERM_MANAGERS_VIEW, <ManagersPage />)} />
          <Route path="roles" element={guard(PERM_MANAGERS_VIEW, <RolesPage />)} />
          <Route path="audit-log" element={guard(PERM_AUDIT_VIEW, <AuditLogPage />)} />
          <Route path="settings/*" element={guard(PERM_SETTINGS_VIEW, <SettingsPage />)} />
          <Route path="license" element={<LicensePage />} />
          <Route path="account" element={<AccountSecurityPage />} />
          <Route path="dev/rtl-smoke" element={<RtlSmokePage />} />
          <Route path="*" element={<NotFoundPage />} />
        </Route>
      </Routes>
    </SetupGate>
  )
}

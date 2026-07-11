import { Route, Routes } from 'react-router-dom'

import { RequireAuth } from './auth/AuthContext'
import { AppShell } from './shell/AppShell'
import { DashboardPage } from './pages/DashboardPage'
import { LoginPage } from './pages/LoginPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { RtlSmokePage } from './pages/RtlSmokePage'
import { LedgerPage } from './pages/billing/LedgerPage'
import { VouchersPage } from './pages/billing/VouchersPage'
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
import { AccountSecurityPage } from './pages/security/AccountSecurityPage'
import { AuditLogPage } from './pages/security/AuditLogPage'
import { ManagersPage } from './pages/security/ManagersPage'
import { RolesPage } from './pages/security/RolesPage'
import { UserDetailPage } from './pages/subscribers/UserDetailPage'
import { UserListPage } from './pages/subscribers/UserListPage'

export function App() {
  return (
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
        <Route path="subscribers" element={<UserListPage />} />
        <Route path="subscribers/:id" element={<UserDetailPage />} />
        <Route path="profiles" element={<ProfilesPage />} />
        <Route path="nas" element={<NasListPage />} />
        <Route path="nas/:id/status" element={<NasStatusPage />} />
        <Route path="pools" element={<PoolsPage />} />
        <Route path="sessions" element={<LiveSessionsPage />} />
        <Route path="ledger" element={<LedgerPage />} />
        <Route path="vouchers" element={<VouchersPage />} />
        <Route path="devices" element={<DevicesPage />} />
        <Route path="devices/:id/status" element={<DeviceStatusPage />} />
        <Route path="health" element={<HealthPage />} />
        <Route path="alerts" element={<AlertsPage />} />
        <Route path="debug" element={<DebugPage />} />
        <Route path="managers" element={<ManagersPage />} />
        <Route path="roles" element={<RolesPage />} />
        <Route path="audit-log" element={<AuditLogPage />} />
        <Route path="account" element={<AccountSecurityPage />} />
        <Route path="dev/rtl-smoke" element={<RtlSmokePage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}

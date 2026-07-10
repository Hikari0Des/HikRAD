import { Route, Routes } from 'react-router-dom'

import { RequireAuth } from './auth/AuthContext'
import { AppShell } from './shell/AppShell'
import { DashboardPage } from './pages/DashboardPage'
import { LoginPage } from './pages/LoginPage'
import { NotFoundPage } from './pages/NotFoundPage'
import { RtlSmokePage } from './pages/RtlSmokePage'
import { LiveSessionsPage } from './pages/live/LiveSessionsPage'
import { NasListPage } from './pages/nas/NasListPage'
import { PoolsPage } from './pages/pools/PoolsPage'
import { ProfilesPage } from './pages/profiles/ProfilesPage'
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
        <Route path="pools" element={<PoolsPage />} />
        <Route path="sessions" element={<LiveSessionsPage />} />
        <Route path="dev/rtl-smoke" element={<RtlSmokePage />} />
        <Route path="*" element={<NotFoundPage />} />
      </Route>
    </Routes>
  )
}

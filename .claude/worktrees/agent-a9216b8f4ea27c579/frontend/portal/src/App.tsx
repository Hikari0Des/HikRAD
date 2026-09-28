import { Navigate, Route, Routes } from 'react-router-dom'

import { RequireAuth } from './auth/AuthContext'
import { HomePage } from './pages/HomePage'
import { InvoicesPage } from './pages/InvoicesPage'
import { LoginPage } from './pages/LoginPage'
import { RegisterPage } from './pages/RegisterPage'
import { RenewPage } from './pages/RenewPage'
import { SettingsPage } from './pages/SettingsPage'
import { TicketsPage } from './pages/TicketsPage'
import { UsagePage } from './pages/UsagePage'
import { PortalLayout } from './shell/PortalLayout'

export function App() {
  return (
    <Routes>
      <Route path="/" element={<LoginPage />} />
      <Route path="/register" element={<RegisterPage />} />
      <Route
        element={
          <RequireAuth>
            <PortalLayout />
          </RequireAuth>
        }
      >
        <Route path="/home" element={<HomePage />} />
        <Route path="/usage" element={<UsagePage />} />
        <Route path="/renew" element={<RenewPage />} />
        <Route path="/tickets" element={<TicketsPage />} />
        <Route path="/invoices" element={<InvoicesPage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

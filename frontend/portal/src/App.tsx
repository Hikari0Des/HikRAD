import { Navigate, Route, Routes } from 'react-router-dom'

import { RequireAuth } from './auth/AuthContext'
import { HomePage } from './pages/HomePage'
import { LoginPage } from './pages/LoginPage'
import { PaymentReturnPage } from './pages/PaymentReturnPage'
import { RenewPage } from './pages/RenewPage'
import { SettingsPage } from './pages/SettingsPage'
import { UsagePage } from './pages/UsagePage'
import { PortalLayout } from './shell/PortalLayout'

export function App() {
  return (
    <Routes>
      <Route path="/" element={<LoginPage />} />
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
        <Route path="/renew/return/:gateway" element={<PaymentReturnPage />} />
        <Route path="/settings" element={<SettingsPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

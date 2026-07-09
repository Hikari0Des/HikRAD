import { Navigate, Route, Routes } from 'react-router-dom'

import { HomePage } from './pages/HomePage'
import { LoginPage } from './pages/LoginPage'
import { RenewPage } from './pages/RenewPage'
import { UsagePage } from './pages/UsagePage'
import { PortalLayout } from './shell/PortalLayout'

export function App() {
  return (
    <Routes>
      <Route path="/" element={<LoginPage />} />
      <Route element={<PortalLayout />}>
        <Route path="/home" element={<HomePage />} />
        <Route path="/usage" element={<UsagePage />} />
        <Route path="/renew" element={<RenewPage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  )
}

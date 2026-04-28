import React, { useEffect, useState } from 'react'
import { Routes, Route, Navigate } from 'react-router-dom'
import Layout from './components/Layout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Providers from './pages/Providers'
import ProviderDetail from './pages/ProviderDetail'
import MediaProviders from './pages/MediaProviders'
import RoutesPage from './pages/Routes'
import Usage from './pages/Usage'
import ProxyPools from './pages/ProxyPools'
import Endpoints from './pages/Endpoints'
import CLITools from './pages/CLITools'
import MITM from './pages/MITM'
import Translator from './pages/Translator'
import Logs from './pages/Logs'
import BasicChat from './pages/BasicChat'
import Profile from './pages/Profile'
import Pricing from './pages/Pricing'
import Settings from './pages/Settings'
import { api } from './api.js'

export default function App() {
  const [isAuthenticated, setIsAuthenticated] = useState(
    !!localStorage.getItem('router_api_key') || false
  )
  const [checkingAuth, setCheckingAuth] = useState(true)
  const [setupStatus, setSetupStatus] = useState(null)

  useEffect(() => {
    const check = async () => {
      try {
        const status = await api.setupStatus()
        setSetupStatus(status)
        if (!status.auth_required) {
          setIsAuthenticated(true)
          return
        }
        if (!localStorage.getItem('router_api_key')) {
          setIsAuthenticated(false)
          return
        }
        await api.config()
        setIsAuthenticated(true)
      } catch (err) {
        if (err.message === 'Unauthorized') {
          setIsAuthenticated(false)
        }
      } finally {
        setCheckingAuth(false)
      }
    }
    check()

    const handleAuthError = () => setIsAuthenticated(false)
    window.addEventListener('auth_error', handleAuthError)
    return () => window.removeEventListener('auth_error', handleAuthError)
  }, [])

  if (checkingAuth) {
    return <div className="min-h-screen bg-gray-950 flex items-center justify-center text-gray-500">Loading...</div>
  }

  if (!isAuthenticated) {
    return <Login setupStatus={setupStatus} onLogin={() => setIsAuthenticated(true)} />
  }

  return (
    <Routes>
      <Route path="/" element={<Layout />}>
        <Route index element={<Navigate to="/dashboard/endpoint" replace />} />
        <Route path="dashboard" element={<Dashboard setupStatus={setupStatus} />} />
        <Route path="dashboard/endpoint" element={<Endpoints />} />
        <Route path="dashboard/providers" element={<Providers />} />
        <Route path="dashboard/providers/:id" element={<ProviderDetail />} />
        <Route path="dashboard/media-providers/:kind" element={<MediaProviders />} />
        <Route path="dashboard/combos" element={<RoutesPage />} />
        <Route path="dashboard/usage" element={<Usage />} />
        <Route path="dashboard/quota" element={<Usage />} />
        <Route path="dashboard/proxy-pools" element={<ProxyPools />} />
        <Route path="dashboard/cli-tools" element={<CLITools />} />
        <Route path="dashboard/mitm" element={<MITM />} />
        <Route path="dashboard/console-log" element={<Logs />} />
        <Route path="dashboard/translator" element={<Translator />} />
        <Route path="dashboard/basic-chat" element={<BasicChat />} />
        <Route path="dashboard/profile" element={<Profile />} />
        <Route path="dashboard/pricing" element={<Pricing />} />
        <Route path="dashboard/settings" element={<Settings />} />
        
        {/* Legacy redirects */}
        <Route path="providers" element={<Navigate to="/dashboard/providers" replace />} />
        <Route path="routes" element={<Navigate to="/dashboard/combos" replace />} />
        <Route path="usage" element={<Navigate to="/dashboard/usage" replace />} />
        <Route path="settings" element={<Navigate to="/dashboard/settings" replace />} />
        <Route path="pricing" element={<Navigate to="/dashboard/pricing" replace />} />
        <Route path="logs" element={<Navigate to="/dashboard/console-log" replace />} />
        <Route path="*" element={<Navigate to="/dashboard/endpoint" replace />} />
      </Route>
    </Routes>
  )
}


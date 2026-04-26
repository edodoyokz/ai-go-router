import React, { useEffect, useState } from 'react'
import { Routes, Route, Navigate, useLocation } from 'react-router-dom'
import Layout from './components/Layout'
import Login from './pages/Login'
import Dashboard from './pages/Dashboard'
import Providers from './pages/Providers'
import RoutesPage from './pages/Routes'
import Models from './pages/Models'
import Logs from './pages/Logs'
import Metrics from './pages/Metrics'
import Usage from './pages/Usage'
import Pricing from './pages/Pricing'
import OAuth from './pages/OAuth'
import Settings from './pages/Settings'

export default function App() {
  const [isAuthenticated, setIsAuthenticated] = useState(
    !!localStorage.getItem('router_api_key') || false
  )
  const [checkingAuth, setCheckingAuth] = useState(true)

  useEffect(() => {
    // Check initial auth state by probing API
    const check = async () => {
      try {
        await fetch('/api/metrics', {
          headers: localStorage.getItem('router_api_key')
            ? { Authorization: `Bearer ${localStorage.getItem('router_api_key')}` }
            : {}
        }).then(r => {
          if (r.status === 401) {
            localStorage.removeItem('router_api_key')
            setIsAuthenticated(false)
          } else {
            setIsAuthenticated(true)
          }
        })
      } catch {
        // Assume failure means unauthenticated if there's an error
      } finally {
        setCheckingAuth(false)
      }
    }
    check()

    // Listen for auth failures from API
    const handleAuthError = () => setIsAuthenticated(false)
    window.addEventListener('auth_error', handleAuthError)
    return () => window.removeEventListener('auth_error', handleAuthError)
  }, [])

  if (checkingAuth) {
    return <div className="min-h-screen bg-gray-950 flex items-center justify-center text-gray-500">Loading...</div>
  }

  if (!isAuthenticated) {
    return <Login onLogin={() => setIsAuthenticated(true)} />
  }

  return (
    <Routes>
      <Route path="/" element={<Layout />}>
        <Route index element={<Dashboard />} />
        <Route path="providers" element={<Providers />} />
        <Route path="routes" element={<RoutesPage />} />
        <Route path="models" element={<Models />} />
        <Route path="logs" element={<Logs />} />
        <Route path="metrics" element={<Metrics />} />
        <Route path="usage" element={<Usage />} />
        <Route path="pricing" element={<Pricing />} />
        <Route path="oauth" element={<OAuth />} />
        <Route path="settings" element={<Settings />} />
        <Route path="*" element={<Navigate to="/" replace />} />
      </Route>
    </Routes>
  )
}

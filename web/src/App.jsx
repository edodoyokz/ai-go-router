import React from 'react'
import { Routes, Route } from 'react-router-dom'
import Layout from './components/Layout.jsx'
import Dashboard from './pages/Dashboard.jsx'
import Providers from './pages/Providers.jsx'
import Routes_ from './pages/Routes.jsx'
import Models from './pages/Models.jsx'
import Logs from './pages/Logs.jsx'
import Metrics from './pages/Metrics.jsx'
import Usage from './pages/Usage.jsx'
import Pricing from './pages/Pricing.jsx'
import OAuth from './pages/OAuth.jsx'
import Settings from './pages/Settings.jsx'

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<Layout />}>
        <Route index element={<Dashboard />} />
        <Route path="providers" element={<Providers />} />
        <Route path="routes" element={<Routes_ />} />
        <Route path="models" element={<Models />} />
        <Route path="logs" element={<Logs />} />
        <Route path="metrics" element={<Metrics />} />
        <Route path="usage" element={<Usage />} />
        <Route path="pricing" element={<Pricing />} />
        <Route path="oauth" element={<OAuth />} />
        <Route path="settings" element={<Settings />} />
      </Route>
    </Routes>
  )
}

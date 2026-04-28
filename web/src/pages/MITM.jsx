import React, { useEffect, useState } from 'react'
import { Shield, Power, AlertCircle, RefreshCw, Server, AlertTriangle } from 'lucide-react'
import { api } from '../api.js'
import { Card, Button, Toggle } from '../components/ui'

export default function MITM() {
  const [status, setStatus] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [actionLoading, setActionLoading] = useState(false)

  const load = () => {
    setLoading(true)
    api.mitmStatus()
      .then(res => setStatus(res))
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  const toggleMitm = async () => {
    setActionLoading(true)
    setError(null)
    try {
      if (status?.running) {
        await api.mitmStop()
      } else {
        await api.mitmStart({})
      }
      load()
    } catch (err) {
      setError(err.message)
    } finally {
      setActionLoading(false)
    }
  }

  const toggleDNS = async () => {
    setActionLoading(true)
    setError(null)
    try {
      await api.mitmPatch({ dns_enabled: !status?.dns_enabled })
      load()
    } catch (err) {
      setError(err.message)
    } finally {
      setActionLoading(false)
    }
  }

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white flex items-center gap-2">
            <Shield size={24} className="text-sky-400" /> Antigravity MITM
          </h1>
          <p className="text-sm text-gray-400 mt-1">Intercept AI desktop client traffic and route it through 9Router</p>
        </div>
        <Button variant="outline" size="sm" onClick={load}>
          <RefreshCw size={16} className="mr-2" /> Refresh
        </Button>
      </div>

      {error && (
        <div className="mb-6 p-4 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm flex items-center gap-2">
          <AlertCircle size={16} /> {error}
        </div>
      )}

      {loading ? (
        <div className="text-gray-500 text-center py-12">Loading...</div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <Card className="p-6">
            <div className="flex items-start justify-between mb-6">
              <div>
                <h2 className="text-lg font-semibold text-white mb-1">MITM Proxy Server</h2>
                <div className="text-sm text-gray-400">Captures HTTPS traffic securely</div>
              </div>
              <div className={`px-3 py-1 rounded-full text-xs font-semibold uppercase tracking-wide flex items-center gap-1.5 ${status?.running ? 'bg-green-500/10 text-green-400 border border-green-500/20' : 'bg-gray-800 text-gray-400'}`}>
                <div className={`w-2 h-2 rounded-full ${status?.running ? 'bg-green-400' : 'bg-gray-500'}`}></div>
                {status?.running ? 'Running' : 'Stopped'}
              </div>
            </div>

            <div className="space-y-4">
              <div className="flex items-center justify-between py-2 border-b border-gray-800">
                <span className="text-sm text-gray-400">Root Certificate</span>
                <span className={`text-sm ${status?.cert_trusted ? 'text-green-400' : status?.cert_installed ? 'text-amber-400' : 'text-red-400'}`}>
                  {status?.cert_trusted ? 'Installed & Trusted' : status?.cert_installed ? 'Installed (Not Trusted)' : 'Missing'}
                </span>
              </div>
              <div className="flex items-center justify-between py-2 border-b border-gray-800">
                <span className="text-sm text-gray-400">Proxy Port</span>
                <span className="text-sm font-mono text-white">443 / 80</span>
              </div>
            </div>

            <div className="mt-8">
              <Button 
                variant={status?.running ? "danger" : "primary"} 
                className="w-full flex justify-center"
                onClick={toggleMitm}
                disabled={actionLoading}
              >
                <Power size={18} className="mr-2" />
                {status?.running ? 'Stop Interception' : 'Start MITM Proxy'}
              </Button>
            </div>
          </Card>

          <Card className="p-6">
            <div className="flex items-start justify-between mb-6">
              <div>
                <h2 className="text-lg font-semibold text-white mb-1">Local DNS Hijack</h2>
                <div className="text-sm text-gray-400">Resolves provider domains to localhost</div>
              </div>
            </div>

            {!status?.running ? (
              <div className="bg-gray-900/50 border border-gray-800 rounded-lg p-4 text-sm text-gray-500 flex items-start gap-3">
                <AlertTriangle className="shrink-0 mt-0.5 text-amber-500/50" size={18} />
                You must start the MITM proxy before enabling DNS hijacking.
              </div>
            ) : (
              <div className="space-y-6">
                <div className="flex items-center justify-between">
                  <div className="text-sm text-gray-300">Hijack AI Domains</div>
                  <Toggle checked={status?.dns_enabled} onChange={toggleDNS} disabled={actionLoading} />
                </div>
                
                <div className="bg-sky-900/10 border border-sky-900/30 rounded-lg p-4 text-sm text-sky-300 flex items-start gap-3">
                  <Server className="shrink-0 mt-0.5" size={18} />
                  <div>
                    <div className="font-semibold mb-1">Supported domains:</div>
                    <ul className="list-disc pl-4 space-y-1 font-mono text-xs opacity-80">
                      <li>api.githubcopilot.com</li>
                      <li>api.anthropic.com</li>
                      <li>api.openai.com</li>
                      <li>cursor.sh</li>
                    </ul>
                  </div>
                </div>
              </div>
            )}
          </Card>
        </div>
      )}
    </div>
  )
}

import React, { useEffect, useState } from 'react'
import { Server, CheckCircle, XCircle, RefreshCw } from 'lucide-react'
import { api } from '../api.js'

export default function Providers() {
  const [providers, setProviders] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  const load = () => {
    setLoading(true)
    api.providers()
      .then(data => setProviders(Array.isArray(data) ? data : data.providers || []))
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  const tierColor = (tier) => {
    switch (tier) {
      case 'primary': return 'bg-sky-500/20 text-sky-300'
      case 'secondary': return 'bg-purple-500/20 text-purple-300'
      case 'fallback': return 'bg-amber-500/20 text-amber-300'
      default: return 'bg-gray-700 text-gray-300'
    }
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white">Providers</h1>
          <p className="text-sm text-gray-400 mt-1">Configured AI provider adapters</p>
        </div>
        <button onClick={load} className="flex items-center gap-2 text-sm text-gray-400 hover:text-white">
          <RefreshCw size={14} /> Refresh
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm">{error}</div>
      )}

      {loading && <div className="text-center py-12 text-gray-500">Loading...</div>}

      <div className="grid gap-4">
        {providers.map((p) => (
          <div key={p.name} className="bg-gray-900 rounded-xl border border-gray-800 p-5">
            <div className="flex items-center gap-3 mb-3">
              <Server size={18} className="text-sky-400 shrink-0" />
              <span className="font-semibold text-white">{p.name}</span>
              {p.enabled ? (
                <CheckCircle size={14} className="text-green-400 ml-auto" />
              ) : (
                <XCircle size={14} className="text-red-400 ml-auto" />
              )}
            </div>
            <div className="grid grid-cols-2 gap-2 text-sm">
              <div>
                <span className="text-gray-500">Type</span>
                <div className="text-gray-200 mt-0.5">{p.type}</div>
              </div>
              <div>
                <span className="text-gray-500">Tier</span>
                <div className="mt-0.5">
                  <span className={`px-2 py-0.5 rounded text-xs font-medium ${tierColor(p.tier)}`}>
                    {p.tier || 'default'}
                  </span>
                </div>
              </div>
              <div className="col-span-2">
                <span className="text-gray-500">Base URL</span>
                <div className="text-gray-300 mt-0.5 font-mono text-xs truncate">{p.base_url}</div>
              </div>
              {p.accounts && p.accounts.length > 0 && (
                <div>
                  <span className="text-gray-500">Accounts</span>
                  <div className="text-gray-200 mt-0.5">{p.accounts.length}</div>
                </div>
              )}
            </div>
          </div>
        ))}
        {!loading && providers.length === 0 && (
          <div className="text-center py-12 text-gray-500">No providers configured</div>
        )}
      </div>
    </div>
  )
}

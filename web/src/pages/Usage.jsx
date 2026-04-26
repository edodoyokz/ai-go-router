import React, { useEffect, useState } from 'react'
import { BarChart2, RefreshCw } from 'lucide-react'
import { api } from '../api.js'

export default function Usage() {
  const [usage, setUsage] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  const load = () => {
    setLoading(true)
    api.usage()
      .then(setUsage)
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  const counters = usage?.counters || usage?.usage || []
  const snapshots = usage?.quota_snapshots || []

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white">Usage</h1>
          <p className="text-sm text-gray-400 mt-1">Token consumption and quota snapshots</p>
        </div>
        <button onClick={load} className="flex items-center gap-2 text-sm text-gray-400 hover:text-white">
          <RefreshCw size={14} /> Refresh
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm">{error}</div>
      )}
      {loading && <div className="text-center py-12 text-gray-500">Loading...</div>}

      {/* Usage counters */}
      {counters.length > 0 && (
        <div className="mb-6">
          <h2 className="text-sm font-semibold text-gray-300 mb-3">Token Counters</h2>
          <div className="bg-gray-900 rounded-xl border border-gray-800 overflow-hidden">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-800 text-gray-400">
                  <th className="px-4 py-3 text-left font-medium">Provider</th>
                  <th className="px-4 py-3 text-left font-medium">Model</th>
                  <th className="px-4 py-3 text-right font-medium">Prompt</th>
                  <th className="px-4 py-3 text-right font-medium">Completion</th>
                  <th className="px-4 py-3 text-right font-medium">Total</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800">
                {counters.map((c, i) => (
                  <tr key={i} className="hover:bg-gray-800/50">
                    <td className="px-4 py-2.5 text-gray-300">{c.provider}</td>
                    <td className="px-4 py-2.5 font-mono text-xs text-gray-400">{c.model}</td>
                    <td className="px-4 py-2.5 text-right text-gray-300">{c.prompt_tokens?.toLocaleString()}</td>
                    <td className="px-4 py-2.5 text-right text-gray-300">{c.completion_tokens?.toLocaleString()}</td>
                    <td className="px-4 py-2.5 text-right text-white font-medium">{c.total_tokens?.toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {/* Quota snapshots */}
      {snapshots.length > 0 && (
        <div>
          <h2 className="text-sm font-semibold text-gray-300 mb-3">Daily Quota Snapshots</h2>
          <div className="bg-gray-900 rounded-xl border border-gray-800 overflow-hidden">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-gray-800 text-gray-400">
                  <th className="px-4 py-3 text-left font-medium">Date</th>
                  <th className="px-4 py-3 text-left font-medium">Provider</th>
                  <th className="px-4 py-3 text-right font-medium">Total Tokens</th>
                  <th className="px-4 py-3 text-right font-medium">Cost (USD)</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-gray-800">
                {snapshots.map((s, i) => (
                  <tr key={i} className="hover:bg-gray-800/50">
                    <td className="px-4 py-2.5 text-gray-400 text-xs font-mono">{s.snapshot_date}</td>
                    <td className="px-4 py-2.5 text-gray-300">{s.provider}</td>
                    <td className="px-4 py-2.5 text-right text-gray-300">{s.total_tokens?.toLocaleString()}</td>
                    <td className="px-4 py-2.5 text-right text-amber-400">${s.cost_usd?.toFixed(4)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {!loading && counters.length === 0 && snapshots.length === 0 && (
        <div className="text-center py-12 text-gray-500">
          <BarChart2 size={32} className="mx-auto mb-3 opacity-30" />
          No usage data yet
        </div>
      )}
    </div>
  )
}

import React, { useEffect, useState } from 'react'
import { Activity, RefreshCw } from 'lucide-react'
import { api } from '../api.js'

function MetricRow({ label, value }) {
  return (
    <div className="flex items-center justify-between py-3 border-b border-gray-800 last:border-0">
      <span className="text-sm text-gray-400">{label}</span>
      <span className="text-sm font-mono text-white">{value ?? '—'}</span>
    </div>
  )
}

export default function Metrics() {
  const [metrics, setMetrics] = useState(null)
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(true)

  const load = () => {
    setLoading(true)
    api.metrics()
      .then(setMetrics)
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  const successRate = metrics
    ? metrics.requests_total > 0
      ? ((metrics.requests_success / metrics.requests_total) * 100).toFixed(1) + '%'
      : '0%'
    : '—'

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white">Metrics</h1>
          <p className="text-sm text-gray-400 mt-1">Real-time performance statistics</p>
        </div>
        <button onClick={load} className="flex items-center gap-2 text-sm text-gray-400 hover:text-white">
          <RefreshCw size={14} /> Refresh
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm">{error}</div>
      )}

      {loading && <div className="text-center py-12 text-gray-500">Loading...</div>}

      {metrics && (
        <div className="space-y-4">
          <div className="bg-gray-900 rounded-xl border border-gray-800 p-5">
            <div className="flex items-center gap-2 mb-2">
              <Activity size={16} className="text-sky-400" />
              <h2 className="text-sm font-semibold text-white">Request Statistics</h2>
            </div>
            <MetricRow label="Total Requests" value={metrics.requests_total} />
            <MetricRow label="Successful" value={metrics.requests_success} />
            <MetricRow label="Errors" value={metrics.requests_error} />
            <MetricRow label="Success Rate" value={successRate} />
            <MetricRow label="Uptime (seconds)" value={metrics.uptime_seconds} />
          </div>

          {metrics.provider_usage && Object.keys(metrics.provider_usage).length > 0 && (
            <div className="bg-gray-900 rounded-xl border border-gray-800 p-5">
              <h2 className="text-sm font-semibold text-white mb-2">Provider Breakdown</h2>
              {Object.entries(metrics.provider_usage).map(([name, count]) => (
                <MetricRow key={name} label={name} value={count} />
              ))}
            </div>
          )}

          {metrics.memory_bytes !== undefined && (
            <div className="bg-gray-900 rounded-xl border border-gray-800 p-5">
              <h2 className="text-sm font-semibold text-white mb-2">Runtime</h2>
              <MetricRow label="Memory" value={`${(metrics.memory_bytes / 1024 / 1024).toFixed(1)} MB`} />
              <MetricRow label="Goroutines" value={metrics.goroutines} />
            </div>
          )}
        </div>
      )}
    </div>
  )
}

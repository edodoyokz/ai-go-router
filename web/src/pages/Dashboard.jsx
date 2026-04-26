import React, { useEffect, useState } from 'react'
import { Activity, CheckCircle, XCircle, Zap, Server, TrendingUp } from 'lucide-react'
import { api } from '../api.js'

function StatCard({ label, value, icon: Icon, color = 'sky' }) {
  const colors = {
    sky: 'text-sky-400 bg-sky-400/10',
    green: 'text-green-400 bg-green-400/10',
    red: 'text-red-400 bg-red-400/10',
    amber: 'text-amber-400 bg-amber-400/10',
  }
  return (
    <div className="bg-gray-900 rounded-xl border border-gray-800 p-5">
      <div className="flex items-center justify-between mb-3">
        <span className="text-sm text-gray-400">{label}</span>
        <span className={`p-2 rounded-lg ${colors[color]}`}>
          <Icon size={16} />
        </span>
      </div>
      <div className="text-2xl font-bold text-white">{value ?? '—'}</div>
    </div>
  )
}

export default function Dashboard() {
  const [metrics, setMetrics] = useState(null)
  const [health, setHealth] = useState(null)
  const [error, setError] = useState(null)

  useEffect(() => {
    Promise.all([api.metrics(), api.health()])
      .then(([m, h]) => { setMetrics(m); setHealth(h) })
      .catch(e => setError(e.message))
  }, [])

  return (
    <div className="p-6">
      <div className="mb-6">
        <h1 className="text-xl font-bold text-white">Dashboard</h1>
        <p className="text-sm text-gray-400 mt-1">9router AI Gateway overview</p>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm">
          {error}
        </div>
      )}

      {/* Health badge */}
      <div className="mb-6 flex items-center gap-2">
        {health ? (
          <span className="flex items-center gap-1.5 text-green-400 text-sm font-medium">
            <CheckCircle size={14} /> Gateway Healthy
          </span>
        ) : error ? (
          <span className="flex items-center gap-1.5 text-red-400 text-sm font-medium">
            <XCircle size={14} /> Gateway Unreachable
          </span>
        ) : (
          <span className="text-gray-500 text-sm">Checking health...</span>
        )}
      </div>

      {/* Stats grid */}
      <div className="grid grid-cols-2 lg:grid-cols-4 gap-4 mb-6">
        <StatCard label="Total Requests" value={metrics?.requests_total} icon={Activity} color="sky" />
        <StatCard label="Successful" value={metrics?.requests_success} icon={CheckCircle} color="green" />
        <StatCard label="Errors" value={metrics?.requests_error} icon={XCircle} color="red" />
        <StatCard label="Uptime" value={metrics?.uptime_seconds ? `${Math.floor(metrics.uptime_seconds / 60)}m` : '—'} icon={TrendingUp} color="amber" />
      </div>

      {/* Provider usage */}
      {metrics?.provider_usage && Object.keys(metrics.provider_usage).length > 0 && (
        <div className="bg-gray-900 rounded-xl border border-gray-800 p-5">
          <div className="flex items-center gap-2 mb-4">
            <Server size={16} className="text-sky-400" />
            <h2 className="text-sm font-semibold text-white">Provider Usage</h2>
          </div>
          <div className="space-y-3">
            {Object.entries(metrics.provider_usage).map(([name, count]) => (
              <div key={name} className="flex items-center gap-3">
                <span className="text-sm text-gray-300 w-32 truncate">{name}</span>
                <div className="flex-1 bg-gray-800 rounded-full h-2">
                  <div
                    className="bg-sky-500 h-2 rounded-full"
                    style={{ width: `${Math.min(100, (count / (metrics.requests_total || 1)) * 100)}%` }}
                  />
                </div>
                <span className="text-sm text-gray-400 w-12 text-right">{count}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {!metrics && !error && (
        <div className="text-center py-12 text-gray-500">Loading...</div>
      )}
    </div>
  )
}

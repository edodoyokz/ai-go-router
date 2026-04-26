import React, { useEffect, useState } from 'react'
import { FileText, RefreshCw, CheckCircle, XCircle } from 'lucide-react'
import { api } from '../api.js'

export default function Logs() {
  const [logs, setLogs] = useState([])
  const [total, setTotal] = useState(0)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [filter, setFilter] = useState({ provider: '', status: '', limit: 50 })

  const load = () => {
    setLoading(true)
    const params = {}
    if (filter.provider) params.provider = filter.provider
    if (filter.status) params.status = filter.status
    params.limit = filter.limit
    api.logs(params)
      .then(data => { setLogs(data.logs || []); setTotal(data.total || 0) })
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(load, [filter])

  const statusBadge = (status) => status === 'success'
    ? <span className="flex items-center gap-1 text-green-400 text-xs"><CheckCircle size={11} />success</span>
    : <span className="flex items-center gap-1 text-red-400 text-xs"><XCircle size={11} />error</span>

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white">Request Logs</h1>
          <p className="text-sm text-gray-400 mt-1">{total} total entries</p>
        </div>
        <button onClick={load} className="flex items-center gap-2 text-sm text-gray-400 hover:text-white">
          <RefreshCw size={14} /> Refresh
        </button>
      </div>

      {/* Filters */}
      <div className="flex gap-3 mb-4">
        <input
          type="text"
          placeholder="Provider..."
          value={filter.provider}
          onChange={e => setFilter(f => ({ ...f, provider: e.target.value }))}
          className="bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-500 focus:outline-none focus:border-sky-500 w-36"
        />
        <select
          value={filter.status}
          onChange={e => setFilter(f => ({ ...f, status: e.target.value }))}
          className="bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white focus:outline-none focus:border-sky-500"
        >
          <option value="">All status</option>
          <option value="success">Success</option>
          <option value="error">Error</option>
        </select>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm">{error}</div>
      )}

      {loading && <div className="text-center py-12 text-gray-500">Loading...</div>}

      <div className="bg-gray-900 rounded-xl border border-gray-800 overflow-hidden">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-gray-800 text-gray-400">
              <th className="px-4 py-3 text-left font-medium">Time</th>
              <th className="px-4 py-3 text-left font-medium">Model</th>
              <th className="px-4 py-3 text-left font-medium">Provider</th>
              <th className="px-4 py-3 text-left font-medium">Status</th>
              <th className="px-4 py-3 text-right font-medium">Duration</th>
              <th className="px-4 py-3 text-right font-medium">Tokens</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-800">
            {logs.map((log, i) => (
              <tr key={log.request_id || i} className="hover:bg-gray-800/50 transition-colors">
                <td className="px-4 py-2.5 text-gray-400 font-mono text-xs">
                  {new Date(log.start_time).toLocaleString()}
                </td>
                <td className="px-4 py-2.5 text-gray-200 font-mono text-xs max-w-[140px] truncate">
                  {log.model}
                </td>
                <td className="px-4 py-2.5 text-gray-300 text-xs">{log.provider}</td>
                <td className="px-4 py-2.5">{statusBadge(log.status)}</td>
                <td className="px-4 py-2.5 text-right text-gray-400 text-xs">
                  {log.duration_ms ?? Math.round((log.duration || 0) / 1e6)}ms
                </td>
                <td className="px-4 py-2.5 text-right text-gray-400 text-xs">
                  {log.total_tokens ?? '—'}
                </td>
              </tr>
            ))}
            {!loading && logs.length === 0 && (
              <tr>
                <td colSpan={6} className="text-center py-8 text-gray-500">No logs found</td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

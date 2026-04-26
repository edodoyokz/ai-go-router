import React, { useEffect, useState } from 'react'
import { DollarSign, RefreshCw } from 'lucide-react'
import { api } from '../api.js'

export default function Pricing() {
  const [pricing, setPricing] = useState(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [filter, setFilter] = useState('')

  const load = () => {
    setLoading(true)
    api.pricing()
      .then(setPricing)
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  const models = pricing?.models || []
  const filtered = filter
    ? models.filter(m => m.model_id?.toLowerCase().includes(filter.toLowerCase()))
    : models

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white">Pricing</h1>
          <p className="text-sm text-gray-400 mt-1">Model cost rates (USD per million tokens)</p>
        </div>
        <button onClick={load} className="flex items-center gap-2 text-sm text-gray-400 hover:text-white">
          <RefreshCw size={14} /> Refresh
        </button>
      </div>

      <input
        type="text"
        placeholder="Filter models..."
        value={filter}
        onChange={e => setFilter(e.target.value)}
        className="w-full mb-4 bg-gray-900 border border-gray-700 rounded-lg px-3 py-2 text-sm text-white placeholder-gray-500 focus:outline-none focus:border-sky-500"
      />

      {error && (
        <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm">{error}</div>
      )}
      {loading && <div className="text-center py-12 text-gray-500">Loading...</div>}

      <div className="bg-gray-900 rounded-xl border border-gray-800 overflow-hidden">
        <table className="w-full text-sm">
          <thead>
            <tr className="border-b border-gray-800 text-gray-400">
              <th className="px-4 py-3 text-left font-medium">Model</th>
              <th className="px-4 py-3 text-right font-medium">Input ($/M)</th>
              <th className="px-4 py-3 text-right font-medium">Output ($/M)</th>
            </tr>
          </thead>
          <tbody className="divide-y divide-gray-800">
            {filtered.map((m, i) => (
              <tr key={i} className="hover:bg-gray-800/50">
                <td className="px-4 py-2.5 font-mono text-xs text-gray-200">{m.model || m.model_id}</td>
                <td className="px-4 py-2.5 text-right text-green-400">
                  ${(m.input_price_per_million || 0).toFixed(2)}
                </td>
                <td className="px-4 py-2.5 text-right text-amber-400">
                  ${(m.output_price_per_million || 0).toFixed(2)}
                </td>
              </tr>
            ))}
            {!loading && filtered.length === 0 && (
              <tr>
                <td colSpan={3} className="text-center py-8 text-gray-500">
                  {error ? 'Failed to load pricing' : 'No pricing data'}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

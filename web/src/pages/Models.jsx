import React, { useEffect, useState } from 'react'
import { Cpu, RefreshCw } from 'lucide-react'
import { api } from '../api.js'

export default function Models() {
  const [models, setModels] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [filter, setFilter] = useState('')

  const load = () => {
    setLoading(true)
    api.models()
      .then(data => setModels(data.data || []))
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  const filtered = filter
    ? models.filter(m => m.id.toLowerCase().includes(filter.toLowerCase()))
    : models

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white">Models</h1>
          <p className="text-sm text-gray-400 mt-1">{models.length} available models</p>
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

      <div className="bg-gray-900 rounded-xl border border-gray-800 divide-y divide-gray-800">
        {filtered.map(m => (
          <div key={m.id} className="flex items-center gap-3 px-5 py-3">
            <Cpu size={14} className="text-sky-400 shrink-0" />
            <span className="font-mono text-sm text-gray-200">{m.id}</span>
            {m.owned_by && (
              <span className="ml-auto text-xs text-gray-500">{m.owned_by}</span>
            )}
          </div>
        ))}
        {!loading && filtered.length === 0 && (
          <div className="text-center py-8 text-gray-500 text-sm">No models found</div>
        )}
      </div>
    </div>
  )
}

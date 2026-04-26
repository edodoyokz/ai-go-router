import React, { useEffect, useState } from 'react'
import { Route, ArrowRight } from 'lucide-react'
import { api } from '../api.js'

export default function Routes() {
  const [config, setConfig] = useState(null)
  const [error, setError] = useState(null)

  useEffect(() => {
    api.config()
      .then(setConfig)
      .catch(e => setError(e.message))
  }, [])

  const routes = config?.routes || {}
  const aliases = config?.model_aliases || {}

  return (
    <div className="p-6">
      <div className="mb-6">
        <h1 className="text-xl font-bold text-white">Routes</h1>
        <p className="text-sm text-gray-400 mt-1">Model routing and fallback chains</p>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm">{error}</div>
      )}

      <div className="space-y-4 mb-8">
        {Object.entries(routes).map(([alias, route]) => (
          <div key={alias} className="bg-gray-900 rounded-xl border border-gray-800 p-5">
            <div className="flex items-center gap-2 mb-3">
              <Route size={16} className="text-sky-400" />
              <span className="font-mono font-semibold text-white">{alias}</span>
              <span className="ml-auto text-xs text-gray-500 bg-gray-800 px-2 py-0.5 rounded">
                {route.strategy || 'fallback'}
              </span>
            </div>
            <div className="flex flex-wrap gap-2 items-center">
              {(route.targets || []).map((t, i) => (
                <React.Fragment key={i}>
                  {i > 0 && <ArrowRight size={12} className="text-gray-600" />}
                  <span className="text-sm bg-gray-800 text-gray-200 px-2.5 py-1 rounded-lg font-mono">
                    {t.provider}/{t.model}
                    {t.tier && <span className="ml-1 text-xs text-gray-500">({t.tier})</span>}
                  </span>
                </React.Fragment>
              ))}
            </div>
          </div>
        ))}
      </div>

      {Object.keys(aliases).length > 0 && (
        <div>
          <h2 className="text-sm font-semibold text-gray-300 mb-3">Model Aliases</h2>
          <div className="bg-gray-900 rounded-xl border border-gray-800 divide-y divide-gray-800">
            {Object.entries(aliases).map(([alias, target]) => (
              <div key={alias} className="flex items-center gap-3 px-5 py-3 text-sm">
                <span className="font-mono text-gray-300 w-40 truncate">{alias}</span>
                <ArrowRight size={12} className="text-gray-600 shrink-0" />
                <span className="font-mono text-sky-400">{target.provider}/{target.model}</span>
              </div>
            ))}
          </div>
        </div>
      )}

      {!config && !error && (
        <div className="text-center py-12 text-gray-500">Loading...</div>
      )}
    </div>
  )
}

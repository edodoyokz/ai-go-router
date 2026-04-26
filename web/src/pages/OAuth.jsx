import React, { useEffect, useState } from 'react'
import { Key, Trash2, RefreshCw, CheckCircle, AlertCircle } from 'lucide-react'
import { api } from '../api.js'

export default function OAuth() {
  const [tokens, setTokens] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [deleting, setDeleting] = useState(null)

  const load = () => {
    setLoading(true)
    api.oauthTokens()
      .then(data => setTokens(data.tokens || []))
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  const handleDelete = async (provider, account) => {
    if (!confirm(`Delete OAuth token for ${provider}/${account}?`)) return
    setDeleting(`${provider}/${account}`)
    try {
      await api.deleteOAuthToken(provider, account)
      await load()
    } catch (e) {
      setError(e.message)
    } finally {
      setDeleting(null)
    }
  }

  const isExpired = (expiresAt) => {
    if (!expiresAt) return false
    return new Date(expiresAt) < new Date()
  }

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white">OAuth Tokens</h1>
          <p className="text-sm text-gray-400 mt-1">Stored provider authentication tokens</p>
        </div>
        <button onClick={load} className="flex items-center gap-2 text-sm text-gray-400 hover:text-white">
          <RefreshCw size={14} /> Refresh
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm flex items-center gap-2">
          <AlertCircle size={14} /> {error}
        </div>
      )}

      {loading && <div className="text-center py-12 text-gray-500">Loading...</div>}

      {!loading && tokens.length === 0 && !error && (
        <div className="text-center py-16 text-gray-500">
          <Key size={32} className="mx-auto mb-3 opacity-30" />
          <p>No OAuth tokens stored</p>
          <p className="text-xs mt-1 text-gray-600">
            Tokens are added when providers complete the OAuth flow
          </p>
        </div>
      )}

      <div className="space-y-3">
        {tokens.map((t) => {
          const expired = isExpired(t.expires_at)
          const key = `${t.provider}/${t.account}`
          return (
            <div key={key} className="bg-gray-900 rounded-xl border border-gray-800 p-5">
              <div className="flex items-center gap-3">
                <Key size={16} className="text-sky-400 shrink-0" />
                <div className="flex-1 min-w-0">
                  <div className="font-semibold text-white">{t.provider}</div>
                  <div className="text-sm text-gray-400">{t.account}</div>
                </div>
                <div className="flex items-center gap-3">
                  {expired ? (
                    <span className="flex items-center gap-1 text-red-400 text-xs">
                      <AlertCircle size={11} /> Expired
                    </span>
                  ) : (
                    <span className="flex items-center gap-1 text-green-400 text-xs">
                      <CheckCircle size={11} /> Valid
                    </span>
                  )}
                  <button
                    onClick={() => handleDelete(t.provider, t.account)}
                    disabled={deleting === key}
                    className="text-red-400 hover:text-red-300 disabled:opacity-40"
                  >
                    <Trash2 size={15} />
                  </button>
                </div>
              </div>
              {t.expires_at && (
                <div className="mt-2 text-xs text-gray-500">
                  Expires: {new Date(t.expires_at).toLocaleString()}
                </div>
              )}
              {t.scope && (
                <div className="mt-1 text-xs text-gray-600 font-mono truncate">{t.scope}</div>
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

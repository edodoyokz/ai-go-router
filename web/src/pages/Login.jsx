import React, { useState } from 'react'
import { Lock, ArrowRight } from 'lucide-react'
import { api } from '../api.js'

export default function Login({ onLogin }) {
  const [token, setToken] = useState('')
  const [error, setError] = useState(null)
  const [loading, setLoading] = useState(false)

  const handleSubmit = async (e) => {
    e.preventDefault()
    if (!token) return

    setLoading(true)
    setError(null)
    
    // Temporarily save to localStorage to test auth
    localStorage.setItem('router_api_key', token)
    
    try {
      // Call config endpoint to verify the key
      await api.config()
      onLogin()
    } catch (err) {
      localStorage.removeItem('router_api_key')
      setError('Invalid API key')
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="min-h-screen bg-gray-950 flex items-center justify-center p-4">
      <div className="bg-gray-900 border border-gray-800 rounded-2xl w-full max-w-md p-8 shadow-2xl">
        <div className="flex flex-col items-center text-center mb-8">
          <div className="w-12 h-12 bg-sky-500/10 text-sky-400 rounded-xl flex items-center justify-center mb-4">
            <Lock size={24} />
          </div>
          <h1 className="text-2xl font-bold text-white">Authentication Required</h1>
          <p className="text-gray-400 text-sm mt-2">
            Please enter your API Key or Admin API Key to access the router dashboard.
          </p>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <div>
            <input
              type="password"
              placeholder="sk_..."
              value={token}
              onChange={(e) => setToken(e.target.value)}
              autoFocus
              className="w-full bg-gray-950 border border-gray-700 rounded-xl px-4 py-3 text-white placeholder-gray-600 focus:outline-none focus:border-sky-500 focus:ring-1 focus:ring-sky-500 transition-all"
            />
          </div>

          {error && (
            <div className="text-red-400 text-sm text-center bg-red-900/20 py-2 rounded-lg border border-red-900/50">
              {error}
            </div>
          )}

          <button
            type="submit"
            disabled={loading || !token}
            className="w-full bg-sky-600 hover:bg-sky-500 disabled:opacity-50 disabled:hover:bg-sky-600 text-white font-medium py-3 rounded-xl flex items-center justify-center gap-2 transition-colors"
          >
            {loading ? 'Verifying...' : 'Access Dashboard'}
            {!loading && <ArrowRight size={18} />}
          </button>
        </form>
      </div>
    </div>
  )
}

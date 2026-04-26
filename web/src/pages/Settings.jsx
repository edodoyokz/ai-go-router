import React, { useEffect, useState } from 'react'
import { Settings as SettingsIcon, Save, RefreshCw, CheckCircle } from 'lucide-react'
import { api } from '../api.js'

function Field({ label, children }) {
  return (
    <div className="flex items-center justify-between py-3 border-b border-gray-800 last:border-0">
      <span className="text-sm text-gray-400 w-48">{label}</span>
      <div className="flex-1 ml-4">{children}</div>
    </div>
  )
}

export default function Settings() {
  const [settings, setSettings] = useState(null)
  const [form, setForm] = useState({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [saved, setSaved] = useState(false)
  const [error, setError] = useState(null)

  const load = () => {
    setLoading(true)
    api.settings()
      .then(data => { setSettings(data); setForm(data) })
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  const handleSave = async () => {
    setSaving(true)
    setError(null)
    try {
      await api.updateSettings(form)
      setSaved(true)
      setTimeout(() => setSaved(false), 2000)
    } catch (e) {
      setError(e.message)
    } finally {
      setSaving(false)
    }
  }

  const set = (key, value) => setForm(f => ({ ...f, [key]: value }))

  return (
    <div className="p-6">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white">Settings</h1>
          <p className="text-sm text-gray-400 mt-1">Gateway configuration</p>
        </div>
        <button onClick={load} className="flex items-center gap-2 text-sm text-gray-400 hover:text-white mr-3">
          <RefreshCw size={14} /> Reload
        </button>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm">{error}</div>
      )}

      {loading && <div className="text-center py-12 text-gray-500">Loading...</div>}

      {settings && (
        <div className="space-y-4">
          {/* General */}
          <div className="bg-gray-900 rounded-xl border border-gray-800 p-5">
            <div className="flex items-center gap-2 mb-3">
              <SettingsIcon size={15} className="text-sky-400" />
              <h2 className="text-sm font-semibold text-white">General</h2>
            </div>

            <Field label="Locale">
              <select
                value={form.locale || 'en'}
                onChange={e => set('locale', e.target.value)}
                className="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm text-white focus:outline-none focus:border-sky-500"
              >
                <option value="en">English</option>
                <option value="id">Indonesian</option>
                <option value="zh">Chinese</option>
                <option value="ja">Japanese</option>
              </select>
            </Field>

            <Field label="Native Passthrough">
              <input
                type="checkbox"
                checked={!!form.native_passthrough}
                onChange={e => set('native_passthrough', e.target.checked)}
                className="w-4 h-4 accent-sky-500"
              />
            </Field>

            <Field label="Combo Strategy">
              <select
                value={form.combo_strategy || 'fallback'}
                onChange={e => set('combo_strategy', e.target.value)}
                className="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm text-white focus:outline-none focus:border-sky-500"
              >
                <option value="fallback">Fallback</option>
                <option value="round-robin">Round Robin</option>
                <option value="lowest-cost">Lowest Cost</option>
              </select>
            </Field>
          </div>

          {/* Thinking */}
          <div className="bg-gray-900 rounded-xl border border-gray-800 p-5">
            <h2 className="text-sm font-semibold text-white mb-3">Thinking / Reasoning</h2>
            <Field label="Enable Thinking">
              <input
                type="checkbox"
                checked={!!form.thinking?.enabled}
                onChange={e => set('thinking', { ...form.thinking, enabled: e.target.checked })}
                className="w-4 h-4 accent-sky-500"
              />
            </Field>
            <Field label="Max Thinking Tokens">
              <input
                type="number"
                value={form.thinking?.max_tokens || 5000}
                onChange={e => set('thinking', { ...form.thinking, max_tokens: parseInt(e.target.value) })}
                className="bg-gray-800 border border-gray-700 rounded px-2 py-1 text-sm text-white w-24 focus:outline-none focus:border-sky-500"
              />
            </Field>
            <Field label="Include Reasoning in Response">
              <input
                type="checkbox"
                checked={!!form.thinking?.include_reasoning}
                onChange={e => set('thinking', { ...form.thinking, include_reasoning: e.target.checked })}
                className="w-4 h-4 accent-sky-500"
              />
            </Field>
          </div>

          {/* Save */}
          <div className="flex justify-end">
            <button
              onClick={handleSave}
              disabled={saving}
              className="flex items-center gap-2 bg-sky-600 hover:bg-sky-500 disabled:opacity-50 text-white text-sm font-medium px-4 py-2 rounded-lg transition-colors"
            >
              {saved ? <CheckCircle size={15} /> : <Save size={15} />}
              {saved ? 'Saved!' : saving ? 'Saving...' : 'Save Settings'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}

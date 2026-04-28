import React, { useEffect, useState } from 'react'
import { Terminal, Settings, RefreshCw, AlertCircle, CheckCircle, Plus } from 'lucide-react'
import { api } from '../api.js'
import { Card, Button, Badge } from '../components/ui'

const SUPPORTED_TOOLS = [
  { id: 'claude', name: 'Claude CLI', desc: 'Anthropic Claude CLI config' },
  { id: 'codex', name: 'GitHub Codex', desc: 'Codex CLI compatibility' },
  { id: 'copilot', name: 'GitHub Copilot', desc: 'Copilot editor integration config' },
  { id: 'droid', name: 'Droid', desc: 'Droid agent config' },
  { id: 'hermes', name: 'Hermes', desc: 'Hermes chat config' },
  { id: 'openclaw', name: 'OpenClaw', desc: 'OpenClaw CLI integration' },
  { id: 'opencode', name: 'OpenCode', desc: 'OpenCode editor config' },
]

export default function CLITools() {
  const [states, setStates] = useState({})
  const [loading, setLoading] = useState(true)
  const [actionLoading, setActionLoading] = useState(null)
  const [error, setError] = useState(null)

  const load = async () => {
    setLoading(true)
    setError(null)
    const newStates = {}
    try {
      for (const tool of SUPPORTED_TOOLS) {
        try {
          const res = await api.cliToolSettings(tool.id)
          newStates[tool.id] = res
        } catch (e) {
          console.error(`Failed to load ${tool.id}:`, e)
        }
      }
      setStates(newStates)
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  const handleConfigure = async (toolId, isConfigured) => {
    setActionLoading(toolId)
    setError(null)
    try {
      if (isConfigured) {
        await api.deleteCliToolSettings(toolId)
      } else {
        // Send a basic setup payload. The backend will usually handle the rest for standard tools.
        await api.updateCliToolSettings(toolId, { enabled: true })
      }
      
      // Reload just this tool's state
      const res = await api.cliToolSettings(toolId)
      setStates(prev => ({ ...prev, [toolId]: res }))
    } catch (e) {
      setError(`Failed to ${isConfigured ? 'remove' : 'apply'} config for ${toolId}: ${e.message}`)
    } finally {
      setActionLoading(null)
    }
  }

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white flex items-center gap-2">
            <Terminal size={24} className="text-sky-400" /> CLI Tools
          </h1>
          <p className="text-sm text-gray-400 mt-1">Configure AI agents and CLIs to route through 9Router</p>
        </div>
        <Button variant="outline" size="sm" onClick={load}>
          <RefreshCw size={16} className="mr-2" /> Refresh
        </Button>
      </div>

      {error && (
        <div className="mb-6 p-4 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm flex items-center gap-2">
          <AlertCircle size={16} /> {error}
        </div>
      )}

      {loading ? (
        <div className="text-gray-500 text-center py-12">Loading...</div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {SUPPORTED_TOOLS.map(tool => {
            const state = states[tool.id] || {}
            const isInstalled = state.installed
            const isConfigured = state.configured || state.has9Router
            
            return (
              <Card key={tool.id} className="p-5 flex flex-col h-full">
                <div className="flex items-start justify-between mb-4">
                  <div className="flex items-center gap-3">
                    <div className="w-10 h-10 rounded-lg bg-gray-900 border border-gray-800 flex items-center justify-center">
                      <Terminal size={18} className={isConfigured ? "text-green-400" : "text-gray-400"} />
                    </div>
                    <div>
                      <h3 className="text-white font-semibold">{tool.name}</h3>
                      <div className="text-xs text-gray-500 mt-0.5">{tool.desc}</div>
                    </div>
                  </div>
                </div>

                <div className="space-y-3 mb-6">
                  <div className="flex items-center justify-between py-2 border-b border-gray-800">
                    <span className="text-sm text-gray-400">Installation</span>
                    <Badge variant={isInstalled ? "success" : "neutral"} size="sm">
                      {isInstalled ? "Detected" : "Not Found"}
                    </Badge>
                  </div>
                  <div className="flex items-center justify-between py-2 border-b border-gray-800">
                    <span className="text-sm text-gray-400">9Router Config</span>
                    <Badge variant={isConfigured ? "success" : "neutral"} size="sm">
                      {isConfigured ? "Active" : "Not Configured"}
                    </Badge>
                  </div>
                  {state.settingsPath || state.configPath ? (
                    <div className="text-xs text-gray-600 font-mono truncate" title={state.settingsPath || state.configPath}>
                      {state.settingsPath || state.configPath}
                    </div>
                  ) : null}
                </div>

                <div className="mt-auto">
                  <Button 
                    variant={isConfigured ? "danger" : "primary"} 
                    className="w-full justify-center"
                    disabled={!isInstalled || actionLoading === tool.id}
                    onClick={() => handleConfigure(tool.id, isConfigured)}
                  >
                    {actionLoading === tool.id ? (
                      <RefreshCw size={16} className="mr-2 animate-spin" />
                    ) : isConfigured ? (
                      <AlertCircle size={16} className="mr-2" />
                    ) : (
                      <Plus size={16} className="mr-2" />
                    )}
                    {actionLoading === tool.id ? 'Working...' : isConfigured ? 'Remove Config' : 'Apply 9Router Config'}
                  </Button>
                </div>
              </Card>
            )
          })}
        </div>
      )}
    </div>
  )
}

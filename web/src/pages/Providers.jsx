import React, { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { Server, Settings, Plus, PlayCircle, AlertCircle, Power, Play } from 'lucide-react'
import { api } from '../api.js'
import { Card, Badge, Button, Toggle } from '../components/ui'

const TAB_ORDER = ['supported', 'api_key', 'free_tier', 'oauth', 'free', 'cookie', 'compatible', 'media', 'planned']
const TAB_LABEL = {
  supported: 'Supported',
  api_key: 'API Key',
  free_tier: 'Free Tier',
  oauth: 'OAuth',
  free: 'Free/No Auth',
  cookie: 'Cookie',
  compatible: 'Compatible',
  media: 'Media/Tools',
  planned: 'Planned',
}

export default function Providers() {
  const [connections, setConnections] = useState([])
  const [catalog, setCatalog] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [activeTab, setActiveTab] = useState('supported')
  const [testing, setTesting] = useState(false)

  const loadData = async () => {
    try {
      setLoading(true)
      const [connRes, catRes] = await Promise.all([
        api.providers(),
        api.providersCatalog({ include_planned: 'true' })
      ])
      setConnections(Array.isArray(connRes?.connections) ? connRes.connections : [])
      setCatalog(Array.isArray(catRes?.providers) ? catRes.providers : [])
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadData()
  }, [])

  const groupedCatalog = useMemo(() => {
    const groups = {}
    TAB_ORDER.forEach(t => groups[t] = [])
    
    catalog.forEach(provider => {
      if (provider.execution_status === 'planned') {
        groups['planned'].push(provider)
        return
      }
      if (provider.service_kinds?.includes('media') || provider.service_kinds?.includes('stt') || provider.service_kinds?.includes('tts') || provider.service_kinds?.includes('image')) {
        groups['media'].push(provider)
        return
      }
      if (provider.execution_kind === 'openai_compatible' || provider.execution_kind === 'ollama') {
        groups['compatible'].push(provider)
        return
      }
      const cat = provider.category || 'other'
      if (groups[cat]) {
        groups[cat].push(provider)
      } else {
        groups['supported'].push(provider)
      }
    })
    
    return groups
  }, [catalog])

  const getStats = (providerId) => {
    const conns = connections.filter(c => c.provider === providerId || c.type === providerId)
    const total = conns.length
    if (total === 0) return { total: 0, connected: 0, error: 0, allDisabled: false }
    
    const allDisabled = conns.every(c => c.enabled === false || c.isActive === false)
    const errorConns = conns.filter(c => c.status === 'error' || c.status === 'unavailable' || c.status === 'expired' || c.lastError)
    const error = errorConns.length
    const connected = conns.filter(c => c.status === 'success' || c.status === 'active' || (!c.lastError && c.enabled !== false)).length
    
    return {
      total,
      connected,
      error,
      allDisabled,
      latestError: errorConns[0]?.lastError || null
    }
  }

  if (loading) {
    return <div className="p-6 text-gray-500">Loading providers...</div>
  }

  if (error) {
    return <div className="p-6 text-red-400">Error loading data: {error}</div>
  }

  const activeProviders = groupedCatalog[activeTab] || []

  return (
    <div className="p-6 max-w-7xl mx-auto">
      <div className="flex items-center justify-between mb-8">
        <div>
          <h1 className="text-2xl font-bold text-white">Providers</h1>
          <p className="text-gray-400 mt-1">Manage AI provider connections and authentication</p>
        </div>
        <div className="flex gap-3">
          <Button variant="outline" size="sm" onClick={loadData}>
            <Settings size={16} className="mr-2" /> Refresh
          </Button>
          <Button variant="primary" size="sm">
            <Plus size={16} className="mr-2" /> Custom Connection
          </Button>
        </div>
      </div>

      <div className="flex border-b border-gray-800 mb-6 overflow-x-auto scrollbar-none">
        {TAB_ORDER.map(tabId => {
          const count = groupedCatalog[tabId]?.length || 0
          if (count === 0 && tabId !== 'supported') return null
          return (
            <button
              key={tabId}
              onClick={() => setActiveTab(tabId)}
              className={`px-4 py-3 text-sm font-medium border-b-2 whitespace-nowrap transition-colors ${
                activeTab === tabId 
                  ? 'border-sky-500 text-sky-400' 
                  : 'border-transparent text-gray-400 hover:text-gray-200 hover:border-gray-700'
              }`}
            >
              {TAB_LABEL[tabId]} <span className="ml-1.5 px-2 py-0.5 rounded-full bg-gray-800 text-xs text-gray-400">{count}</span>
            </button>
          )
        })}
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
        {activeProviders.map(provider => {
          const stats = getStats(provider.id)
          return (
            <Link key={provider.id} to={`/dashboard/providers/${provider.id}`} className="block group">
              <Card hoverable className={`h-full p-4 flex flex-col ${stats.allDisabled ? 'opacity-60' : ''}`}>
                <div className="flex items-start justify-between mb-4">
                  <div className="flex items-center gap-3">
                    <div className="w-10 h-10 rounded-lg bg-gray-800 flex items-center justify-center overflow-hidden border border-gray-700">
                      <span className="text-sm font-bold text-gray-500 uppercase">{provider.id.slice(0,2)}</span>
                    </div>
                    <div>
                      <h3 className="text-white font-semibold group-hover:text-sky-400 transition-colors">{provider.name}</h3>
                      <div className="text-xs text-gray-500 mt-0.5 capitalize">{provider.category}</div>
                    </div>
                  </div>
                  {stats.total > 0 && (
                    <div className="opacity-0 group-hover:opacity-100 transition-opacity">
                      <Settings size={16} className="text-gray-500" />
                    </div>
                  )}
                </div>

                <div className="mt-auto pt-4 flex flex-wrap gap-2">
                  {stats.total === 0 ? (
                    <Badge variant="neutral" size="sm">Not connected</Badge>
                  ) : stats.allDisabled ? (
                    <Badge variant="neutral" size="sm"><Power size={12} className="mr-1" /> Disabled</Badge>
                  ) : (
                    <>
                      {stats.connected > 0 && <Badge variant="success" size="sm" dot>{stats.connected} Active</Badge>}
                      {stats.error > 0 && <Badge variant="error" size="sm" dot>{stats.error} Error</Badge>}
                    </>
                  )}
                  {provider.deprecated && <Badge variant="warning" size="sm">Deprecated</Badge>}
                </div>
              </Card>
            </Link>
          )
        })}
      </div>

      {activeProviders.length === 0 && (
        <div className="py-12 text-center border border-gray-800 rounded-xl bg-gray-900/50">
          <div className="text-gray-500 mb-2">No providers found in this category.</div>
        </div>
      )}
    </div>
  )
}

import React, { useEffect, useState, useRef } from 'react'
import { useParams, useNavigate, Link } from 'react-router-dom'
import { ArrowLeft, Plus, Settings, Trash2, CheckCircle, XCircle, RefreshCw, Power, ExternalLink, Key } from 'lucide-react'
import { api } from '../api.js'
import { Card, Badge, Button, Input, Modal, Select, Toggle } from '../components/ui'

export default function ProviderDetail() {
  const { id } = useParams()
  const navigate = useNavigate()
  
  const [loading, setLoading] = useState(true)
  const [catalogItem, setCatalogItem] = useState(null)
  const [connections, setConnections] = useState([])
  const [models, setModels] = useState([])
  const [proxyPools, setProxyPools] = useState([])
  
  const [showEditModal, setShowEditModal] = useState(false)
  const [editing, setEditing] = useState(null)

  const [showOauthModal, setShowOauthModal] = useState(false)
  const [oauthData, setOauthData] = useState(null)
  const [oauthError, setOauthError] = useState(null)
  const pollIntervalRef = useRef(null)
  
  const [form, setForm] = useState({
    name: '',
    authType: 'apikey',
    apiKey: '',
    defaultModel: '',
    priority: 1,
    globalPriority: 0,
    connectionProxyEnabled: false,
    connectionProxyUrl: '',
    connectionNoProxy: '',
    proxyPoolId: '__none__',
    isActive: true,
    clearSecret: false,
    providerSpecificData: {}
  })
  
  const [error, setError] = useState('')
  const [notice, setNotice] = useState('')

  const loadData = async () => {
    setLoading(true)
    try {
      const [catRes, connsRes, modelsRes, poolsRes] = await Promise.all([
        api.providersCatalog({ include_planned: 'true' }),
        api.providers(),
        api.providerModels(id).catch(() => []),
        api.proxyPools().catch(() => ({ pools: [] }))
      ])
      
      const item = (catRes?.providers || []).find(p => p.id === id)
      if (!item) {
        setError('Provider not found in catalog')
      } else {
        setCatalogItem(item)
      }
      
      setConnections((connsRes?.connections || []).filter(c => c.provider === id || c.type === id))
      
      const mData = Array.isArray(modelsRes?.data) ? modelsRes.data : Array.isArray(modelsRes?.models) ? modelsRes.models : []
      setModels(mData.map(m => (typeof m === 'string' ? { id: m, name: m } : { id: m.id || m.name, name: m.name || m.id })))
      
      setProxyPools(poolsRes?.pools || [])
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadData()
  }, [id])

  useEffect(() => {
    if (notice || error) {
      const t = setTimeout(() => { setNotice(''); setError('') }, 5000)
      return () => clearTimeout(t)
    }
  }, [notice, error])

  const openWizard = () => {
    const allowedAuthTypes = catalogItem ? catalogItem.auth_types : ['apikey']
    const firstAuthType = allowedAuthTypes.length > 0 ? allowedAuthTypes[0] : 'apikey'
    setEditing(null)
    setForm({
      name: '',
      authType: firstAuthType === 'api_key' ? 'apikey' : firstAuthType,
      apiKey: '',
      defaultModel: '',
      priority: 1,
      globalPriority: 0,
      connectionProxyEnabled: false,
      connectionProxyUrl: '',
      connectionNoProxy: '',
      proxyPoolId: '__none__',
      isActive: true,
      clearSecret: false,
      providerSpecificData: {}
    })
    setShowEditModal(true)
  }

  const openEdit = (conn) => {
    setEditing(conn)
    setForm({
      name: conn.name || '',
      authType: conn.auth_type === 'api_key' ? 'apikey' : (conn.auth_type || 'apikey'),
      apiKey: '', 
      defaultModel: conn.default_model || '',
      priority: typeof conn.priority === 'number' ? conn.priority : 1,
      globalPriority: typeof conn.global_priority === 'number' ? conn.global_priority : 0,
      connectionProxyEnabled: conn.connection_proxy_enabled || false,
      connectionProxyUrl: conn.connection_proxy_url || '',
      connectionNoProxy: conn.connection_no_proxy || '',
      proxyPoolId: conn.proxy_pool_id || '__none__',
      isActive: conn.is_active !== false,
      clearSecret: false,
      providerSpecificData: conn.provider_specific_data || {}
    })
    setShowEditModal(true)
  }

  const submitForm = async (e) => {
    if (e) e.preventDefault()
    
    const payload = {
      provider: id,
      name: form.name || 'Default',
      auth_type: form.authType === 'apikey' ? 'api_key' : form.authType,
      default_model: form.defaultModel,
      priority: Number(form.priority),
      global_priority: Number(form.globalPriority),
      connection_proxy_enabled: form.connectionProxyEnabled,
      connection_proxy_url: form.connectionProxyUrl,
      connection_no_proxy: form.connectionNoProxy,
      proxy_pool_id: form.proxyPoolId === '__none__' ? '' : form.proxyPoolId,
      is_active: form.isActive,
      provider_specific_data: form.providerSpecificData
    }

    if (form.clearSecret) {
      payload.api_key = ''
    } else if (form.apiKey.trim() !== '') {
      payload.api_key = form.apiKey.trim()
    }

    try {
      if (editing) {
        await api.updateProvider(editing.id || editing.name, payload)
        setNotice('Connection updated')
      } else {
        await api.createProvider(payload)
        setNotice('Connection created')
      }
      setShowEditModal(false)
      loadData()
    } catch (e) {
      setError(e.message)
    }
  }

  const deleteConnection = async (conn) => {
    if (!window.confirm(`Delete connection ${conn.name}?`)) return
    try {
      await api.deleteProvider(conn.id || conn.name)
      setNotice('Connection deleted')
      loadData()
    } catch (e) {
      setError(e.message)
    }
  }

  const testConnection = async (conn) => {
    try {
      const res = await api.testProvider(conn.id || conn.name)
      if (res.valid) setNotice('Connection valid')
      else setNotice(`Test failed: ${res.error || 'unknown error'}`)
      loadData()
    } catch (e) {
      setError(e.message)
    }
  }

  const startOAuth = async (conn) => {
    try {
      setOauthError(null)
      setShowOauthModal(true)
      const res = await api.oauthDeviceCode(conn.provider || id, { account: conn.name })
      setOauthData({ ...res, connName: conn.name })
      
      // Start polling
      if (pollIntervalRef.current) clearInterval(pollIntervalRef.current)
      pollIntervalRef.current = setInterval(async () => {
        try {
          const pollRes = await api.oauthPoll(conn.provider || id, { device_code: res.device_code, account: conn.name })
          if (pollRes.status === 'success') {
            clearInterval(pollIntervalRef.current)
            setShowOauthModal(false)
            setNotice(`OAuth successful for ${conn.name}`)
            loadData()
          } else if (pollRes.status === 'error') {
            clearInterval(pollIntervalRef.current)
            setOauthError(pollRes.error || 'OAuth polling failed')
          }
        } catch (e) {
          // ignore polling errors unless fatal
        }
      }, (res.interval || 5) * 1000)
    } catch (e) {
      setOauthError(e.message)
    }
  }

  const closeOauthModal = () => {
    if (pollIntervalRef.current) clearInterval(pollIntervalRef.current)
    setShowOauthModal(false)
    setOauthData(null)
  }

  if (loading) return <div className="p-6 text-gray-500">Loading...</div>

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <div className="flex items-center gap-4 mb-8">
        <Link to="/dashboard/providers" className="p-2 rounded-lg bg-gray-900 border border-gray-800 text-gray-400 hover:text-white hover:border-gray-700 transition-colors">
          <ArrowLeft size={18} />
        </Link>
        <div className="flex items-center gap-3">
          <div className="w-12 h-12 rounded-lg bg-gray-900 border border-gray-800 flex items-center justify-center shrink-0">
            <span className="text-gray-500 font-bold uppercase">{id.slice(0,2)}</span>
          </div>
          <div>
            <h1 className="text-2xl font-bold text-white">{catalogItem?.name || id}</h1>
            <div className="text-sm text-gray-500 font-mono">{id} • {catalogItem?.category || 'custom'}</div>
          </div>
        </div>
      </div>

      {notice && <div className="mb-6 p-4 rounded-xl bg-green-500/10 border border-green-500/20 text-green-400">{notice}</div>}
      {error && <div className="mb-6 p-4 rounded-xl bg-red-500/10 border border-red-500/20 text-red-400">{error}</div>}

      <div className="flex items-center justify-between mb-4">
        <h2 className="text-lg font-semibold text-white">Connections</h2>
        <Button onClick={openWizard} variant="primary" size="sm">
          <Plus size={16} className="mr-2" /> Add Connection
        </Button>
      </div>

      {connections.length === 0 ? (
        <Card className="p-12 text-center border-dashed border-gray-800 bg-transparent flex flex-col items-center justify-center">
          <div className="text-gray-500 mb-4">No connections configured yet.</div>
          <Button onClick={openWizard} variant="outline">Create Connection</Button>
        </Card>
      ) : (
        <div className="space-y-4">
          {connections.map((conn, i) => (
            <Card key={i} className={`p-4 flex flex-col md:flex-row gap-4 justify-between items-start md:items-center ${conn.is_active === false ? 'opacity-60' : ''}`}>
              <div className="flex flex-col gap-1">
                <div className="flex items-center gap-2">
                  <span className="font-semibold text-white">{conn.name || 'Unnamed'}</span>
                  {conn.is_active === false ? (
                    <Badge variant="neutral" size="sm">Disabled</Badge>
                  ) : conn.status === 'success' ? (
                    <Badge variant="success" size="sm" dot>OK</Badge>
                  ) : conn.status === 'error' ? (
                    <Badge variant="error" size="sm" dot title={conn.last_error}>Error</Badge>
                  ) : (
                    <Badge variant="neutral" size="sm">Untested</Badge>
                  )}
                </div>
                <div className="text-xs text-gray-500 flex gap-2">
                  <span className="uppercase tracking-wide">{conn.auth_type}</span>
                  {conn.default_model && <span>• Model: {conn.default_model}</span>}
                </div>
              </div>
              <div className="flex gap-2">
                {conn.auth_type === 'oauth' && (
                  <Button variant="outline" size="sm" onClick={() => startOAuth(conn)} title="Authenticate">
                    <Key size={16} className="mr-2" /> Auth
                  </Button>
                )}
                <Button variant="ghost" size="sm" onClick={() => testConnection(conn)} title="Test">
                  <RefreshCw size={16} />
                </Button>
                <Button variant="ghost" size="sm" onClick={() => openEdit(conn)} title="Edit">
                  <Settings size={16} />
                </Button>
                <Button variant="ghost" size="sm" onClick={() => deleteConnection(conn)} className="text-red-400 hover:text-red-300 hover:bg-red-900/30" title="Delete">
                  <Trash2 size={16} />
                </Button>
              </div>
            </Card>
          ))}
        </div>
      )}

      <Modal 
        isOpen={showEditModal} 
        onClose={() => setShowEditModal(false)}
        title={editing ? "Edit Connection" : "New Connection"}
        size="md"
        footer={
          <>
            <Button variant="ghost" onClick={() => setShowEditModal(false)}>Cancel</Button>
            <Button variant="primary" onClick={submitForm}>Save Connection</Button>
          </>
        }
      >
        <form onSubmit={submitForm} className="space-y-4">
          <Input label="Name" value={form.name} onChange={e => setForm({...form, name: e.target.value})} placeholder="Main Account" required />
          
          <Select label="Auth Type" value={form.authType} onChange={e => setForm({...form, authType: e.target.value})} required>
            <option value="apikey">API Key / Token</option>
            <option value="oauth">OAuth</option>
            <option value="cookie">Cookie</option>
            <option value="no_auth">No Auth / Local</option>
            <option value="service_account">Service Account JSON</option>
          </Select>

          {form.authType !== 'no_auth' && form.authType !== 'oauth' && (
            <div className="flex flex-col gap-1">
              <label className="text-sm font-medium text-gray-300">Secret / Credential</label>
              <div className="flex gap-2">
                <input 
                  type="password" 
                  value={form.apiKey} 
                  onChange={e => setForm({...form, apiKey: e.target.value})} 
                  placeholder={editing && !form.clearSecret ? "•••••••• (unchanged)" : "Enter secret"} 
                  className="flex h-10 flex-1 rounded-md border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-sky-500/50 disabled:opacity-50" 
                  disabled={form.clearSecret} 
                  required={!editing && form.authType !== 'no_auth'} 
                />
                {editing && (
                  <Button type="button" variant={form.clearSecret ? "danger" : "outline"} onClick={() => setForm({...form, clearSecret: !form.clearSecret})}>
                    Clear
                  </Button>
                )}
              </div>
            </div>
          )}

          <div className="grid grid-cols-2 gap-4">
            <Input label="Priority (Local)" type="number" value={form.priority} onChange={e => setForm({...form, priority: parseInt(e.target.value) || 1})} min="1" helpText="Lower = higher priority" />
            <Input label="Global Priority" type="number" value={form.globalPriority} onChange={e => setForm({...form, globalPriority: parseInt(e.target.value) || 0})} min="0" helpText="Overrides combo routing if > 0" />
          </div>

          <div className="flex flex-col gap-1 relative">
            <label className="text-sm font-medium text-gray-300">Default Model</label>
            <div className="relative">
              <input 
                type="text"
                value={form.defaultModel} 
                onChange={e => setForm({...form, defaultModel: e.target.value})} 
                placeholder="e.g. gpt-4o"
                className="flex h-10 w-full rounded-md border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-white focus:outline-none focus:ring-2 focus:ring-sky-500/50" 
              />
              {models.length > 0 && (
                <div className="absolute right-2 top-2">
                  <select 
                    className="w-5 h-6 opacity-0 absolute right-0 cursor-pointer"
                    onChange={e => setForm({...form, defaultModel: e.target.value})}
                    value=""
                  >
                    <option value="" disabled>Select a model...</option>
                    {models.map(m => <option key={m.id} value={m.id}>{m.name}</option>)}
                  </select>
                  <Button variant="ghost" size="sm" className="h-6 px-2 pointer-events-none">▼</Button>
                </div>
              )}
            </div>
            <div className="text-xs text-gray-500">Overrides provider's default model for empty model requests</div>
          </div>

          {(id === 'vertex' || id === 'gemini-cli' || id === 'vertex-partner') && (
            <Input label="GCP Project ID" value={form.providerSpecificData?.project_id || ''} onChange={e => setForm({...form, providerSpecificData: {...form.providerSpecificData, project_id: e.target.value}})} placeholder="my-gcp-project" />
          )}

          {form.authType === 'oauth' && (
            <div className="p-4 bg-sky-900/10 border border-sky-900/30 rounded-lg space-y-3 mt-4">
              <h3 className="text-sm font-semibold text-sky-400">OAuth / Device Flow</h3>
              <p className="text-xs text-gray-400">To authenticate with OAuth, you first need to save the connection. Then use the "Authenticate" button from the connection list.</p>
            </div>
          )}

          <div className="pt-4 border-t border-gray-800">
            <Toggle 
              label="Enable per-connection proxy" 
              checked={form.connectionProxyEnabled} 
              onChange={v => setForm({...form, connectionProxyEnabled: v})} 
            />
            
            {form.connectionProxyEnabled && (
              <div className="mt-4 p-4 rounded-lg bg-gray-900/50 border border-gray-800 space-y-4">
                <Input label="Proxy URL" value={form.connectionProxyUrl} onChange={e => setForm({...form, connectionProxyUrl: e.target.value})} placeholder="http://proxy:8080" />
                <Input label="No Proxy" value={form.connectionNoProxy} onChange={e => setForm({...form, connectionNoProxy: e.target.value})} placeholder="localhost,127.0.0.1" />
              </div>
            )}
          </div>
          
          <Select label="Proxy Pool" value={form.proxyPoolId} onChange={e => setForm({...form, proxyPoolId: e.target.value})}>
            <option value="__none__">None (Direct or Global Proxy)</option>
            {proxyPools.map(p => (
              <option key={p.id} value={p.id}>{p.name}</option>
            ))}
          </Select>

          <div className="pt-4 border-t border-gray-800">
            <Toggle 
              label="Connection Enabled" 
              checked={form.isActive} 
              onChange={v => setForm({...form, isActive: v})} 
            />
          </div>
        </form>
      </Modal>

      <Modal 
        isOpen={showOauthModal} 
        onClose={closeOauthModal}
        title="OAuth Authentication"
      >
        <div className="space-y-6">
          {oauthError && (
            <div className="p-4 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm flex items-start gap-2">
              <XCircle size={16} className="mt-0.5 shrink-0" />
              <div>
                <p className="font-semibold mb-1">Authentication Failed</p>
                <p>{oauthError}</p>
              </div>
            </div>
          )}
          
          {!oauthError && oauthData && (
            <>
              <div className="text-center space-y-4">
                <p className="text-gray-300 text-sm">
                  To authenticate <strong>{oauthData.connName}</strong>, please visit the authorization URL below and enter the code.
                </p>
                
                <div className="bg-gray-950 border border-gray-800 rounded-xl p-6">
                  <p className="text-xs text-gray-500 uppercase tracking-widest font-semibold mb-2">Your Device Code</p>
                  <p className="text-3xl font-mono text-sky-400 tracking-[0.2em]">{oauthData.user_code}</p>
                </div>
              </div>

              <div className="flex justify-center">
                <Button 
                  variant="primary" 
                  onClick={() => window.open(oauthData.verification_uri, '_blank')}
                  className="w-full flex justify-center py-3"
                >
                  Open Authorization Page <ExternalLink size={16} className="ml-2" />
                </Button>
              </div>

              <div className="flex items-center justify-center gap-2 text-sm text-gray-500 pt-4">
                <RefreshCw size={14} className="animate-spin text-sky-500" />
                Waiting for you to complete authorization...
              </div>
            </>
          )}
        </div>
      </Modal>
    </div>
  )
}


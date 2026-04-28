import React, { useEffect, useState } from 'react'
import { Network, Plus, Settings, Trash2, CheckCircle, RefreshCw, AlertCircle } from 'lucide-react'
import { api } from '../api.js'
import { Modal, Button, Input, Card, Badge, Toggle, Select } from '../components/ui'

export default function ProxyPools() {
  const [pools, setPools] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  
  const [showModal, setShowModal] = useState(false)
  const [editing, setEditing] = useState(null)
  const [form, setForm] = useState({ name: '', strategy: 'round_robin', proxy_urls: [] })
  const [proxyUrlsText, setProxyUrlsText] = useState('')

  const load = () => {
    setLoading(true)
    api.proxyPools()
      .then(res => setPools(res.pools || []))
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  const openWizard = () => {
    setEditing(null)
    setForm({ name: '', strategy: 'round_robin', proxy_urls: [] })
    setProxyUrlsText('')
    setShowModal(true)
  }

  const openEdit = (pool) => {
    setEditing(pool)
    setForm({ name: pool.name, strategy: pool.strategy || 'round_robin', proxy_urls: pool.proxy_urls || [] })
    setProxyUrlsText((pool.proxy_urls || []).join('\\n'))
    setShowModal(true)
  }

  const handleSubmit = async (e) => {
    e.preventDefault()
    try {
      const urls = proxyUrlsText.split('\\n').map(s => s.trim()).filter(Boolean)
      const payload = { name: form.name, strategy: form.strategy, proxy_urls: urls }
      
      if (editing) {
        await api.updateProxyPool(editing.id, payload)
      } else {
        await api.createProxyPool(payload)
      }
      setShowModal(false)
      load()
    } catch (err) {
      setError(err.message)
    }
  }

  const handleDelete = async (pool) => {
    if (!confirm(`Delete proxy pool ${pool.name}?`)) return
    try {
      await api.deleteProxyPool(pool.id)
      load()
    } catch (err) {
      setError(err.message)
    }
  }

  const testPool = async (pool) => {
    try {
      await api.testProxyPool(pool.id)
      load() // Refresh to see updated health states
    } catch (err) {
      setError(err.message)
    }
  }

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white">Proxy Pools</h1>
          <p className="text-sm text-gray-400 mt-1">Manage outbound HTTP/SOCKS proxies for providers</p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={load}>
            <RefreshCw size={16} className="mr-2" /> Refresh
          </Button>
          <Button variant="primary" size="sm" onClick={openWizard}>
            <Plus size={16} className="mr-2" /> Create Pool
          </Button>
        </div>
      </div>

      {error && (
        <div className="mb-6 p-4 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm flex items-center gap-2">
          <AlertCircle size={16} /> {error}
        </div>
      )}

      {loading ? (
        <div className="text-gray-500 text-center py-12">Loading...</div>
      ) : pools.length === 0 ? (
        <Card className="p-12 text-center border-dashed border-gray-800 bg-transparent flex flex-col items-center justify-center">
          <Network size={32} className="text-gray-600 mb-4" />
          <div className="text-gray-500 mb-4">No proxy pools configured.</div>
          <Button onClick={openWizard} variant="outline">Create your first proxy pool</Button>
        </Card>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          {pools.map(pool => (
            <Card key={pool.id} className="p-5 flex flex-col h-full">
              <div className="flex items-start justify-between mb-4">
                <div className="flex items-center gap-3">
                  <div className="w-10 h-10 rounded-lg bg-gray-900 border border-gray-800 flex items-center justify-center">
                    <Network size={18} className="text-sky-400" />
                  </div>
                  <div>
                    <h3 className="text-white font-semibold">{pool.name}</h3>
                    <div className="text-xs text-gray-500 mt-0.5">{pool.strategy} • {pool.proxy_urls?.length || 0} nodes</div>
                  </div>
                </div>
                <div className="flex gap-2">
                  <Button variant="ghost" size="sm" onClick={() => testPool(pool)} title="Test Proxies">
                    <RefreshCw size={16} />
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => openEdit(pool)} title="Edit">
                    <Settings size={16} />
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => handleDelete(pool)} className="text-red-400 hover:text-red-300 hover:bg-red-900/30" title="Delete">
                    <Trash2 size={16} />
                  </Button>
                </div>
              </div>

              <div className="mt-auto space-y-2">
                <div className="text-xs font-semibold text-gray-400 uppercase tracking-wider mb-2">Proxy Nodes</div>
                <div className="bg-gray-950 rounded-lg border border-gray-800 overflow-hidden max-h-48 overflow-y-auto">
                  {(pool.proxy_urls || []).length === 0 ? (
                    <div className="text-xs text-gray-500 p-3 text-center">No nodes configured</div>
                  ) : (
                    <ul className="divide-y divide-gray-800">
                      {(pool.proxy_urls || []).map((url, i) => (
                        <li key={i} className="text-xs text-gray-300 font-mono p-2.5 flex justify-between items-center">
                          <span className="truncate">{url}</span>
                          <Badge variant="neutral" size="sm">Ready</Badge>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>
              </div>
            </Card>
          ))}
        </div>
      )}

      <Modal 
        isOpen={showModal} 
        onClose={() => setShowModal(false)}
        title={editing ? "Edit Proxy Pool" : "Create Proxy Pool"}
        size="md"
        footer={
          <>
            <Button variant="ghost" onClick={() => setShowModal(false)}>Cancel</Button>
            <Button variant="primary" onClick={handleSubmit}>Save Pool</Button>
          </>
        }
      >
        <form onSubmit={handleSubmit} className="space-y-4">
          <Input 
            label="Pool Name" 
            value={form.name} 
            onChange={e => setForm({...form, name: e.target.value})} 
            placeholder="e.g. US Residential Proxies" 
            required 
          />
          <Select label="Routing Strategy" value={form.strategy} onChange={e => setForm({...form, strategy: e.target.value})}>
            <option value="round_robin">Round Robin</option>
            <option value="random">Random</option>
          </Select>
          <div className="flex flex-col gap-1">
            <label className="text-sm font-medium text-gray-300 flex justify-between">
              Proxy URLs 
              <span className="text-gray-500 text-xs font-normal">One per line</span>
            </label>
            <textarea
              value={proxyUrlsText}
              onChange={e => setProxyUrlsText(e.target.value)}
              placeholder="http://user:pass@proxy1.example.com:8080&#10;socks5://192.168.1.1:1080"
              className="w-full h-32 bg-gray-950 border border-gray-700 rounded-lg px-3 py-2 text-sm font-mono text-white placeholder-gray-600 focus:outline-none focus:border-sky-500 resize-none"
            />
          </div>
        </form>
      </Modal>
    </div>
  )
}

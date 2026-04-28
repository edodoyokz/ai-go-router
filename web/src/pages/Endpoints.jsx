import React, { useEffect, useState } from 'react'
import { Terminal, Key, Plus, Trash2, CheckCircle, Copy, AlertCircle } from 'lucide-react'
import { api } from '../api.js'
import { Modal, Button, Input, Card } from '../components/ui'

export default function Endpoints() {
  const [keys, setKeys] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [showModal, setShowModal] = useState(false)
  const [form, setForm] = useState({ name: '' })
  const [newKey, setNewKey] = useState('')
  const [copied, setCopied] = useState(false)

  const load = () => {
    setLoading(true)
    api.keys()
      .then(data => setKeys(data.keys || []))
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(load, [])

  const handleCreate = async (e) => {
    e.preventDefault()
    try {
      const res = await api.createKey({ name: form.name })
      setNewKey(res.api_key || res.key)
      setForm({ name: '' })
      load()
    } catch (err) {
      setError(err.message)
    }
  }

  const handleDelete = async (id, name) => {
    if (!confirm(`Delete key ${name}?`)) return
    try {
      await api.deleteKey(id)
      load()
    } catch (err) {
      setError(err.message)
    }
  }

  const copyToClipboard = (text) => {
    navigator.clipboard.writeText(text)
    setCopied(true)
    setTimeout(() => setCopied(false), 2000)
  }

  const origin = window.location.origin
  const exampleCode = `curl ${origin}/v1/chat/completions \\
  -H "Content-Type: application/json" \\
  -H "Authorization: Bearer YOUR_API_KEY" \\
  -d '{
    "model": "gpt-4o",
    "messages": [{"role": "user", "content": "Hello!"}]
  }'`

  return (
    <div className="p-6 max-w-5xl mx-auto">
      <div className="mb-6">
        <h1 className="text-xl font-bold text-white">Endpoint & Keys</h1>
        <p className="text-sm text-gray-400 mt-1">Connect your apps to the gateway</p>
      </div>

      {error && (
        <div className="mb-4 p-3 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm flex items-center gap-2">
          <AlertCircle size={14} /> {error}
        </div>
      )}

      <div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
        <div className="lg:col-span-2 space-y-6">
          <Card className="p-5">
            <h2 className="text-lg font-semibold text-white mb-4 flex items-center gap-2">
              <Terminal size={18} className="text-sky-400" /> API Base URL
            </h2>
            <div className="bg-gray-950 border border-gray-800 rounded-lg p-3 flex items-center justify-between font-mono text-sm text-sky-300">
              <span>{origin}/v1</span>
              <button onClick={() => copyToClipboard(`${origin}/v1`)} className="text-gray-500 hover:text-white" title="Copy">
                <Copy size={16} />
              </button>
            </div>
            
            <h3 className="text-sm font-medium text-gray-400 mt-6 mb-2">Example Request</h3>
            <pre className="bg-gray-950 border border-gray-800 rounded-lg p-4 text-xs text-gray-300 overflow-x-auto">
              {exampleCode}
            </pre>
          </Card>

          <Card className="p-5">
            <div className="flex items-center justify-between mb-4">
              <h2 className="text-lg font-semibold text-white flex items-center gap-2">
                <Key size={18} className="text-sky-400" /> Client API Keys
              </h2>
              <Button size="sm" onClick={() => { setShowModal(true); setNewKey(''); setForm({ name: '' }) }}>
                <Plus size={16} className="mr-2" /> Create Key
              </Button>
            </div>

            {loading ? (
              <div className="text-gray-500 text-center py-6">Loading...</div>
            ) : keys.length === 0 ? (
              <div className="text-gray-500 text-center py-6 bg-gray-900/50 rounded-lg border border-dashed border-gray-800">
                No API keys created yet. Any client can connect without authentication.
              </div>
            ) : (
              <div className="divide-y divide-gray-800 bg-gray-950 rounded-lg border border-gray-800">
                {keys.map((key) => (
                  <div key={key.id} className="p-4 flex items-center justify-between">
                    <div>
                      <div className="font-medium text-white">{key.name}</div>
                      <div className="text-xs text-gray-500 mt-1 font-mono">
                        {key.prefix}...{key.suffix}
                      </div>
                    </div>
                    <div className="flex items-center gap-4">
                      <div className="text-xs text-gray-500">
                        Created: {new Date(key.created_at).toLocaleDateString()}
                      </div>
                      <button onClick={() => handleDelete(key.id, key.name)} className="text-gray-500 hover:text-red-400">
                        <Trash2 size={16} />
                      </button>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Card>
        </div>

        <div className="space-y-6">
          <Card className="p-5 bg-sky-900/10 border-sky-900/30">
            <h3 className="font-semibold text-white mb-2">Integration Info</h3>
            <ul className="text-sm text-gray-400 space-y-3">
              <li>• The gateway is fully compatible with OpenAI SDKs (Python, Node.js).</li>
              <li>• Simply set your `baseURL` to the URL shown here.</li>
              <li>• If you create any API keys, the gateway will reject unauthorized requests.</li>
            </ul>
          </Card>
        </div>
      </div>

      <Modal isOpen={showModal} onClose={() => setShowModal(false)} title="Create API Key">
        {newKey ? (
          <div className="space-y-4">
            <div className="bg-green-500/10 border border-green-500/30 text-green-400 p-4 rounded-lg flex items-start gap-3">
              <CheckCircle className="shrink-0 mt-0.5" size={18} />
              <div className="text-sm">
                <p className="font-semibold mb-1">Key Created Successfully</p>
                <p>Please copy this key now. You won't be able to see it again!</p>
              </div>
            </div>
            
            <div className="flex gap-2">
              <input 
                readOnly 
                value={newKey} 
                className="flex-1 bg-gray-950 border border-gray-700 rounded-lg px-3 py-2 text-sm font-mono text-white"
              />
              <Button onClick={() => copyToClipboard(newKey)}>
                {copied ? <CheckCircle size={16} /> : <Copy size={16} />}
              </Button>
            </div>
            
            <div className="flex justify-end pt-4">
              <Button onClick={() => setShowModal(false)}>Close</Button>
            </div>
          </div>
        ) : (
          <form onSubmit={handleCreate} className="space-y-4">
            <Input 
              label="Key Name" 
              value={form.name} 
              onChange={e => setForm({ name: e.target.value })} 
              placeholder="e.g. My Next.js App" 
              required 
            />
            <div className="flex justify-end gap-2 pt-4 border-t border-gray-800">
              <Button type="button" variant="ghost" onClick={() => setShowModal(false)}>Cancel</Button>
              <Button type="submit" variant="primary">Create Key</Button>
            </div>
          </form>
        )}
      </Modal>
    </div>
  )
}

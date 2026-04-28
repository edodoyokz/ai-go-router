import React, { useState, useEffect } from 'react'
import { Languages, Play, ArrowRight, Code, AlertCircle } from 'lucide-react'
import { api } from '../api.js'
import { Card, Button, Input, Select } from '../components/ui'

export default function Translator() {
  const [provider, setProvider] = useState('')
  const [model, setModel] = useState('')
  const [sourceFormat, setSourceFormat] = useState('openai')
  const [targetFormat, setTargetFormat] = useState('anthropic')
  const [payload, setPayload] = useState('{\n  "model": "gpt-4o",\n  "messages": [\n    {"role": "user", "content": "Hello"}\n  ]\n}')
  
  const [translated, setTranslated] = useState('')
  const [response, setResponse] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)
  
  const [providers, setProviders] = useState([])

  useEffect(() => {
    api.providers().then(res => {
      const conns = res.connections || []
      setProviders(conns.filter(c => c.is_active !== false))
      if (conns.length > 0) setProvider(conns[0].id)
    }).catch(console.error)
  }, [])

  const handleTranslate = async () => {
    setLoading(true)
    setError(null)
    setTranslated('')
    setResponse('')
    try {
      const body = JSON.parse(payload)
      const res = await api.translatorTranslate({
        step: 2,
        body: body,
        provider,
        model,
        sourceFormat,
        targetFormat
      })
      if (res.error) throw new Error(res.error)
      setTranslated(JSON.stringify(res.translated || res, null, 2))
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  const handleSend = async () => {
    setLoading(true)
    setError(null)
    setResponse('')
    try {
      const body = JSON.parse(payload)
      const res = await api.translatorSend({
        body: body,
        provider,
        model,
        sourceFormat
      })
      if (res.error) throw new Error(res.error)
      setResponse(JSON.stringify(res.response || res, null, 2))
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div className="p-6 max-w-6xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white flex items-center gap-2">
            <Languages size={24} className="text-sky-400" /> Format Translator
          </h1>
          <p className="text-sm text-gray-400 mt-1">Preview how requests are translated before sending to providers</p>
        </div>
      </div>

      {error && (
        <div className="mb-6 p-4 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm flex items-center gap-2">
          <AlertCircle size={16} /> {error}
        </div>
      )}

      <Card className="p-5 mb-6">
        <div className="grid grid-cols-1 md:grid-cols-4 gap-4 mb-4">
          <Select label="Provider" value={provider} onChange={e => setProvider(e.target.value)}>
            <option value="">Select Provider</option>
            {providers.map(p => (
              <option key={p.id} value={p.id}>{p.name}</option>
            ))}
          </Select>
          <Input label="Model (optional)" value={model} onChange={e => setModel(e.target.value)} placeholder="e.g. claude-3-5-sonnet" />
          <Select label="Source Format" value={sourceFormat} onChange={e => setSourceFormat(e.target.value)}>
            <option value="openai">OpenAI (Chat)</option>
            <option value="anthropic">Anthropic (Messages)</option>
            <option value="openai-completion">OpenAI (Completion)</option>
          </Select>
          <Select label="Target Format" value={targetFormat} onChange={e => setTargetFormat(e.target.value)}>
            <option value="anthropic">Anthropic</option>
            <option value="openai">OpenAI</option>
            <option value="gemini">Gemini</option>
            <option value="cohere">Cohere</option>
            <option value="ollama">Ollama</option>
          </Select>
        </div>

        <div className="flex gap-3">
          <Button variant="primary" onClick={handleTranslate} disabled={loading}>
            <Code size={16} className="mr-2" /> Preview Translation
          </Button>
          <Button variant="outline" onClick={handleSend} disabled={loading || !provider}>
            <Play size={16} className="mr-2 text-green-400" /> Send Live Request
          </Button>
        </div>
      </Card>

      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        <div className="space-y-2">
          <h3 className="text-sm font-semibold text-gray-300">Original Payload ({sourceFormat})</h3>
          <textarea
            value={payload}
            onChange={e => setPayload(e.target.value)}
            className="w-full h-96 bg-gray-950 border border-gray-800 rounded-lg p-4 font-mono text-xs text-sky-200 focus:outline-none focus:border-sky-500 resize-none"
            spellCheck="false"
          />
        </div>

        <div className="space-y-6">
          {translated && (
            <div className="space-y-2">
              <h3 className="text-sm font-semibold text-gray-300 flex items-center gap-2">
                <ArrowRight size={14} className="text-sky-500" /> Translated Payload ({targetFormat})
              </h3>
              <pre className="w-full h-40 bg-gray-950 border border-gray-800 rounded-lg p-4 font-mono text-xs text-green-300 overflow-auto">
                {translated}
              </pre>
            </div>
          )}

          {response && (
            <div className="space-y-2">
              <h3 className="text-sm font-semibold text-gray-300 flex items-center gap-2">
                <ArrowRight size={14} className="text-sky-500" /> Provider Response
              </h3>
              <pre className="w-full h-48 bg-gray-950 border border-gray-800 rounded-lg p-4 font-mono text-xs text-amber-300 overflow-auto">
                {response}
              </pre>
            </div>
          )}
          
          {!translated && !response && (
            <div className="h-96 border-2 border-dashed border-gray-800 rounded-lg flex items-center justify-center text-gray-500 text-sm">
              Results will appear here
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

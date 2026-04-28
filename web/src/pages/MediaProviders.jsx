import React, { useEffect, useState } from 'react'
import { useParams, Link } from 'react-router-dom'
import { Image, Mic, Headphones, Cpu, RefreshCw, AlertCircle, PlayCircle } from 'lucide-react'
import { api } from '../api.js'
import { Card, Badge, Button } from '../components/ui'

const KINDS = {
  embedding: { label: 'Embedding Models', icon: Cpu, desc: 'Generate vector embeddings from text' },
  image: { label: 'Image Generation', icon: Image, desc: 'Generate or edit images from text' },
  tts: { label: 'Text-to-Speech', icon: Headphones, desc: 'Synthesize speech from text' },
  stt: { label: 'Speech-to-Text', icon: Mic, desc: 'Transcribe audio to text' },
}

export default function MediaProviders() {
  const { kind } = useParams()
  const [providers, setProviders] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  const config = KINDS[kind] || KINDS.embedding
  const Icon = config.icon

  const load = () => {
    setLoading(true)
    api.providersCatalog()
      .then(res => {
        const cat = res.catalog || res.providers || []
        // Filter catalog items that support this kind
        const filtered = cat.filter(p => p.service_kinds?.includes(kind))
        setProviders(filtered)
      })
      .catch(e => setError(e.message))
      .finally(() => setLoading(false))
  }

  useEffect(load, [kind])

  return (
    <div className="p-6 max-w-6xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white flex items-center gap-2">
            <Icon size={24} className="text-sky-400" /> {config.label}
          </h1>
          <p className="text-sm text-gray-400 mt-1">{config.desc}</p>
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
        <div className="text-gray-500 text-center py-12">Loading…</div>
      ) : providers.length === 0 ? (
        <div className="text-gray-500 text-center py-12 bg-gray-900/50 rounded-xl border border-dashed border-gray-800">
          <Icon size={32} className="mx-auto mb-3 opacity-30" />
          No providers found supporting {kind}.
        </div>
      ) : (
        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
          {providers.map(provider => (
            <Link key={provider.id} to={`/dashboard/providers/${provider.id}`} className="block group">
              <Card hoverable className="h-full p-4 flex flex-col">
                <div className="flex items-start gap-3 mb-4">
                  <div className="w-10 h-10 rounded-lg bg-gray-800 flex items-center justify-center overflow-hidden border border-gray-700 shrink-0">
                    <span className="text-sm font-bold text-gray-500 uppercase">{provider.id.slice(0,2)}</span>
                  </div>
                  <div>
                    <h3 className="text-white font-semibold group-hover:text-sky-400 transition-colors">{provider.name}</h3>
                    <div className="text-xs text-gray-500 mt-0.5 capitalize">{provider.category || 'Media'}</div>
                  </div>
                </div>

                <div className="mt-auto pt-4 flex flex-wrap gap-2">
                  <Badge variant="neutral" size="sm">Go to Config <ArrowRight size={12} className="ml-1 inline" /></Badge>
                </div>
              </Card>
            </Link>
          ))}
        </div>
      )}
    </div>
  )
}

function ArrowRight(props) {
  return <svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" {...props}><path d="M5 12h14"/><path d="m12 5 7 7-7 7"/></svg>
}

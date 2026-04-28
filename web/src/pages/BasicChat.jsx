import React, { useState, useEffect, useRef } from 'react'
import { MessageSquare, Send, Bot, User, AlertCircle, Trash2 } from 'lucide-react'
import { api } from '../api.js'
import { Card, Input, Button, Select } from '../components/ui'

export default function BasicChat() {
  const [models, setModels] = useState([])
  const [model, setModel] = useState('')
  const [messages, setMessages] = useState([])
  const [input, setInput] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)
  const messagesEndRef = useRef(null)

  useEffect(() => {
    api.models().then(res => {
      const list = res.data || []
      setModels(list)
      if (list.length > 0) setModel(list[0].id)
    }).catch(e => setError("Failed to load models: " + e.message))
  }, [])

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: "smooth" })
  }, [messages])

  const handleSend = async (e) => {
    e.preventDefault()
    if (!input.trim() || loading || !model) return

    const newMsg = { role: 'user', content: input }
    const updatedMessages = [...messages, newMsg]
    setMessages(updatedMessages)
    setInput('')
    setLoading(true)
    setError(null)

    try {
      const response = await fetch('/v1/chat/completions', {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'Authorization': `Bearer ${localStorage.getItem('router_api_key') || ''}`
        },
        body: JSON.stringify({
          model: model,
          messages: updatedMessages
        })
      })

      if (!response.ok) {
        const errData = await response.json().catch(() => ({}))
        throw new Error(errData.error?.message || errData.error || response.statusText)
      }

      const data = await response.json()
      if (data.choices && data.choices[0] && data.choices[0].message) {
        setMessages([...updatedMessages, data.choices[0].message])
      } else {
        throw new Error("Invalid response format")
      }
    } catch (e) {
      setError(e.message)
    } finally {
      setLoading(false)
    }
  }

  const handleClear = () => {
    if (confirm('Clear chat history?')) {
      setMessages([])
      setError(null)
    }
  }

  return (
    <div className="p-6 max-w-4xl mx-auto h-[calc(100vh-theme(spacing.16))] flex flex-col">
      <div className="flex items-center justify-between mb-4 shrink-0">
        <div>
          <h1 className="text-xl font-bold text-white flex items-center gap-2">
            <MessageSquare size={24} className="text-sky-400" /> Basic Chat
          </h1>
          <p className="text-sm text-gray-400 mt-1">Test your router models</p>
        </div>
        <div className="flex items-center gap-3">
          <Select value={model} onChange={e => setModel(e.target.value)} disabled={loading}>
            {models.map(m => (
              <option key={m.id} value={m.id}>{m.id}</option>
            ))}
          </Select>
          <Button variant="outline" size="sm" onClick={handleClear} disabled={messages.length === 0 || loading}>
            <Trash2 size={16} className="mr-2" /> Clear
          </Button>
        </div>
      </div>

      {error && (
        <div className="mb-4 p-4 bg-red-900/30 border border-red-700 rounded-lg text-red-300 text-sm flex items-center gap-2 shrink-0">
          <AlertCircle size={16} /> {error}
        </div>
      )}

      <Card className="flex-1 flex flex-col overflow-hidden border-gray-800">
        <div className="flex-1 overflow-y-auto p-4 space-y-6">
          {messages.length === 0 ? (
            <div className="h-full flex flex-col items-center justify-center text-gray-500">
              <Bot size={48} className="opacity-20 mb-4" />
              <p>Send a message to start chatting with {model || 'a model'}</p>
            </div>
          ) : (
            messages.map((msg, i) => (
              <div key={i} className={`flex gap-4 ${msg.role === 'user' ? 'justify-end' : 'justify-start'}`}>
                {msg.role !== 'user' && (
                  <div className="w-8 h-8 rounded-lg bg-sky-900/50 border border-sky-800 flex items-center justify-center shrink-0">
                    <Bot size={18} className="text-sky-400" />
                  </div>
                )}
                <div className={`max-w-[80%] rounded-xl p-4 text-sm whitespace-pre-wrap ${
                  msg.role === 'user' 
                    ? 'bg-sky-600 text-white rounded-tr-none' 
                    : 'bg-gray-900 border border-gray-800 text-gray-200 rounded-tl-none'
                }`}>
                  {msg.content}
                </div>
                {msg.role === 'user' && (
                  <div className="w-8 h-8 rounded-lg bg-gray-800 border border-gray-700 flex items-center justify-center shrink-0">
                    <User size={18} className="text-gray-400" />
                  </div>
                )}
              </div>
            ))
          )}
          {loading && (
            <div className="flex gap-4 justify-start">
              <div className="w-8 h-8 rounded-lg bg-sky-900/50 border border-sky-800 flex items-center justify-center shrink-0">
                <Bot size={18} className="text-sky-400" />
              </div>
              <div className="max-w-[80%] rounded-xl p-4 text-sm bg-gray-900 border border-gray-800 text-gray-400 rounded-tl-none flex items-center gap-2">
                <span className="animate-pulse">●</span>
                <span className="animate-pulse delay-75">●</span>
                <span className="animate-pulse delay-150">●</span>
              </div>
            </div>
          )}
          <div ref={messagesEndRef} />
        </div>

        <div className="p-4 border-t border-gray-800 bg-gray-950 shrink-0">
          <form onSubmit={handleSend} className="flex gap-2">
            <input
              type="text"
              value={input}
              onChange={e => setInput(e.target.value)}
              placeholder="Type your message..."
              disabled={loading || !model}
              className="flex-1 bg-gray-900 border border-gray-800 rounded-lg px-4 py-2 text-sm text-white placeholder-gray-500 focus:outline-none focus:border-sky-500"
            />
            <Button type="submit" variant="primary" disabled={!input.trim() || loading || !model}>
              <Send size={18} />
            </Button>
          </form>
        </div>
      </Card>
    </div>
  )
}

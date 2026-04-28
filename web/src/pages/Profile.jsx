import React, { useEffect, useState } from 'react'
import { User, Shield, AlertCircle } from 'lucide-react'
import { api } from '../api.js'
import { Card } from '../components/ui'

export default function Profile() {
  const [config, setConfig] = useState(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api.config()
      .then(setConfig)
      .catch(console.error)
      .finally(() => setLoading(false))
  }, [])

  return (
    <div className="p-6 max-w-4xl mx-auto">
      <div className="flex items-center justify-between mb-6">
        <div>
          <h1 className="text-xl font-bold text-white flex items-center gap-2">
            <User size={24} className="text-sky-400" /> Admin Profile
          </h1>
          <p className="text-sm text-gray-400 mt-1">Manage your local gateway access</p>
        </div>
      </div>

      <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
        <Card className="p-6">
          <div className="flex items-center gap-4 mb-6">
            <div className="w-16 h-16 rounded-full bg-sky-900/30 border border-sky-800 flex items-center justify-center text-sky-400 shrink-0">
              <Shield size={28} />
            </div>
            <div>
              <h2 className="text-lg font-semibold text-white">Local Administrator</h2>
              <p className="text-sm text-gray-500">Full system access</p>
            </div>
          </div>
          
          <div className="space-y-4">
            <div className="flex items-center justify-between py-3 border-b border-gray-800">
              <span className="text-sm text-gray-400">Authentication</span>
              <span className="text-sm font-medium text-white">API Key / Token</span>
            </div>
            <div className="flex items-center justify-between py-3 border-b border-gray-800">
              <span className="text-sm text-gray-400">Status</span>
              <span className="text-sm font-medium text-green-400">Active</span>
            </div>
          </div>

          <div className="mt-6 p-4 bg-gray-950 border border-gray-800 rounded-lg text-sm text-gray-400">
            <p>9Router operates as a local-first gateway. There are no remote accounts or cloud logins required to use the core router features.</p>
          </div>
        </Card>
      </div>
    </div>
  )
}

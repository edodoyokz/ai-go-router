import React, { useState, useEffect } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router-dom'
import {
  Server, Route, FileText, BarChart2,
  Settings, Globe, Cpu, Menu, X, Activity, LogOut,
  Shield, Terminal, Network, Languages, User, ChevronDown, Image, Mic, Headphones
} from 'lucide-react'
import clsx from 'clsx'

const navItems = [
  { to: '/dashboard/endpoint', label: 'Endpoint', icon: Globe },
  { to: '/dashboard/providers', label: 'Providers', icon: Server },
  { to: '/dashboard/combos', label: 'Combos', icon: Route },
  { to: '/dashboard/usage', label: 'Usage', icon: BarChart2 },
  { to: '/dashboard/basic-chat', label: 'Basic Chat', icon: Globe },
  { to: '/dashboard/quota', label: 'Quota Tracker', icon: Activity },
  { to: '/dashboard/mitm', label: 'MITM', icon: Shield },
  { to: '/dashboard/cli-tools', label: 'CLI Tools', icon: Terminal },
]

const debugItems = [
  { to: '/dashboard/console-log', label: 'Console Log', icon: FileText },
  { to: '/dashboard/translator', label: 'Translator', icon: Languages },
  { to: '/dashboard/metrics', label: 'Metrics', icon: Activity },
  { to: '/dashboard/oauth', label: 'OAuth Tokens', icon: Terminal },
  { to: '/dashboard/models', label: 'Models', icon: Cpu },
]

const systemItems = [
  { to: '/dashboard/proxy-pools', label: 'Proxy Pools', icon: Network },
]

const mediaKinds = [
  { id: 'embedding', label: 'Embedding', icon: Cpu },
  { id: 'image', label: 'Image', icon: Image },
  { id: 'tts', label: 'Text-to-Speech', icon: Headphones },
  { id: 'stt', label: 'Speech-to-Text', icon: Mic },
]

function NavItem({ item, sidebarOpen, isNested }) {
  return (
    <NavLink
      to={item.to}
      end={item.to === '/dashboard/endpoint'}
      className={({ isActive }) =>
        clsx(
          'flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors',
          isNested && 'pl-10',
          isActive ? 'bg-sky-500/10 text-sky-400' : 'text-gray-400 hover:text-gray-200 hover:bg-gray-800'
        )
      }
    >
      <item.icon size={18} />
      {sidebarOpen && item.label}
    </NavLink>
  )
}

function NavGroup({ title, items, sidebarOpen }) {
  if (!items || items.length === 0) return null
  return (
    <div className="mb-6">
      {sidebarOpen && <h3 className="px-3 mb-2 text-xs font-semibold text-gray-500 uppercase tracking-wider">{title}</h3>}
      <div className="space-y-1">
        {items.map(item => <NavItem key={item.to} item={item} sidebarOpen={sidebarOpen} />)}
      </div>
    </div>
  )
}

export default function Layout() {
  const [sidebarOpen, setSidebarOpen] = useState(true)
  const [mediaOpen, setMediaOpen] = useState(false)
  const location = useLocation()

  useEffect(() => {
    if (location.pathname.includes('/media-providers/')) {
      setMediaOpen(true)
    }
  }, [location.pathname])

  return (
    <div className="flex h-screen overflow-hidden bg-gray-950">
      {/* Sidebar */}
      <aside className={clsx(
        'flex flex-col bg-gray-900 border-r border-gray-800 transition-all duration-200',
        sidebarOpen ? 'w-64' : 'w-14'
      )}>
        <div className="flex items-center gap-3 px-4 py-4 border-b border-gray-800">
          <Globe className="text-sky-400 shrink-0" size={22} />
          {sidebarOpen && (
            <span className="font-bold text-white text-sm tracking-wide">9Router</span>
          )}
          <button
            onClick={() => setSidebarOpen(v => !v)}
            className="ml-auto text-gray-400 hover:text-white"
            aria-label={sidebarOpen ? 'Collapse sidebar' : 'Expand sidebar'}
          >
            {sidebarOpen ? <X size={16} /> : <Menu size={16} />}
          </button>
        </div>

        <nav className="flex-1 py-4 px-2 overflow-y-auto scrollbar-thin">
          <NavGroup title="Main" items={navItems} sidebarOpen={sidebarOpen} />
          
          <div className="mb-6">
            <button
              onClick={() => setMediaOpen(!mediaOpen)}
              className="flex w-full items-center justify-between px-3 py-2 rounded-lg text-sm font-medium text-gray-400 hover:text-gray-200 hover:bg-gray-800 transition-colors"
              aria-label="Toggle media providers navigation"
            >
              <div className="flex items-center gap-3">
                <Image size={18} />
                {sidebarOpen && <span>Media Providers</span>}
              </div>
              {sidebarOpen && (
                <ChevronDown size={14} className={clsx("transition-transform", mediaOpen && "rotate-180")} />
              )}
            </button>
            {mediaOpen && sidebarOpen && (
              <div className="mt-1 space-y-1">
                {mediaKinds.map(kind => (
                  <NavItem key={kind.id} item={{ to: `/dashboard/media-providers/${kind.id}`, label: kind.label, icon: kind.icon }} sidebarOpen={sidebarOpen} isNested />
                ))}
              </div>
            )}
          </div>

          <NavGroup title="System" items={systemItems} sidebarOpen={sidebarOpen} />
          <NavGroup title="Debug" items={debugItems} sidebarOpen={sidebarOpen} />
        </nav>
        
        <div className="p-4 border-t border-gray-800 space-y-1">
          <NavLink
            to="/dashboard/profile"
            className={({ isActive }) =>
              clsx(
                'flex items-center gap-3 px-3 py-2 w-full rounded-lg text-sm font-medium transition-colors',
                isActive ? 'bg-sky-500/10 text-sky-400' : 'text-gray-400 hover:text-gray-200 hover:bg-gray-800'
              )
            }
          >
            <User size={18} />
            {sidebarOpen && 'Profile'}
          </NavLink>
          <NavLink
            to="/dashboard/settings"
            className={({ isActive }) =>
              clsx(
                'flex items-center gap-3 px-3 py-2 w-full rounded-lg text-sm font-medium transition-colors',
                isActive ? 'bg-sky-500/10 text-sky-400' : 'text-gray-400 hover:text-gray-200 hover:bg-gray-800'
              )
            }
          >
            <Settings size={18} />
            {sidebarOpen && 'Settings'}
          </NavLink>
          <button
            onClick={() => {
              localStorage.removeItem('router_api_key')
              window.location.reload()
            }}
            className="flex items-center gap-3 px-3 py-2 w-full rounded-lg text-sm font-medium text-gray-400 hover:text-red-400 hover:bg-red-950/30 transition-colors"
          >
            <LogOut size={18} />
            {sidebarOpen && 'Logout'}
          </button>
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  )
}

import React, { useState } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import {
  LayoutDashboard, Server, Route, FileText, BarChart2,
  DollarSign, Settings, Key, Globe, Cpu, Menu, X, Activity
} from 'lucide-react'
import clsx from 'clsx'

const navItems = [
  { to: '/',         label: 'Dashboard',  icon: LayoutDashboard },
  { to: '/providers',label: 'Providers',  icon: Server },
  { to: '/routes',   label: 'Routes',     icon: Route },
  { to: '/models',   label: 'Models',     icon: Cpu },
  { to: '/logs',     label: 'Logs',       icon: FileText },
  { to: '/metrics',  label: 'Metrics',    icon: Activity },
  { to: '/usage',    label: 'Usage',      icon: BarChart2 },
  { to: '/pricing',  label: 'Pricing',    icon: DollarSign },
  { to: '/oauth',    label: 'OAuth',      icon: Key },
  { to: '/settings', label: 'Settings',   icon: Settings },
]

export default function Layout() {
  const [sidebarOpen, setSidebarOpen] = useState(true)

  return (
    <div className="flex h-screen overflow-hidden bg-gray-950">
      {/* Sidebar */}
      <aside className={clsx(
        'flex flex-col bg-gray-900 border-r border-gray-800 transition-all duration-200',
        sidebarOpen ? 'w-56' : 'w-14'
      )}>
        <div className="flex items-center gap-3 px-4 py-4 border-b border-gray-800">
          <Globe className="text-sky-400 shrink-0" size={22} />
          {sidebarOpen && (
            <span className="font-bold text-white text-sm tracking-wide">9router</span>
          )}
          <button
            onClick={() => setSidebarOpen(v => !v)}
            className="ml-auto text-gray-400 hover:text-white"
          >
            {sidebarOpen ? <X size={16} /> : <Menu size={16} />}
          </button>
        </div>

        <nav className="flex-1 py-2 overflow-y-auto scrollbar-thin">
          {navItems.map(({ to, label, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              end={to === '/'}
              className={({ isActive }) => clsx(
                'flex items-center gap-3 px-4 py-2.5 text-sm transition-colors',
                isActive
                  ? 'bg-sky-600/20 text-sky-400 border-r-2 border-sky-400'
                  : 'text-gray-400 hover:text-white hover:bg-gray-800'
              )}
            >
              <Icon size={17} className="shrink-0" />
              {sidebarOpen && label}
            </NavLink>
          ))}
        </nav>

        <div className="px-4 py-3 border-t border-gray-800 text-xs text-gray-600">
          {sidebarOpen ? 'AI Gateway' : ''}
        </div>
      </aside>

      {/* Main content */}
      <main className="flex-1 overflow-y-auto">
        <Outlet />
      </main>
    </div>
  )
}

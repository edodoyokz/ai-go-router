import React, { useState } from 'react'
import { NavLink, Outlet } from 'react-router-dom'
import {
  LayoutDashboard, Server, Route, FileText, BarChart2,
  DollarSign, Settings, Key, Globe, Cpu, Menu, X, Activity, LogOut
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
            <span className="font-bold text-white text-sm tracking-wide">Router</span>
          )}
          <button
            onClick={() => setSidebarOpen(v => !v)}
            className="ml-auto text-gray-400 hover:text-white"
          >
            {sidebarOpen ? <X size={16} /> : <Menu size={16} />}
          </button>
        </div>

        <nav className="flex-1 py-2 overflow-y-auto scrollbar-thin">
          {navItems.map((item) => (
            <NavLink
              key={item.to}
              to={item.to}
              end={item.to === '/'}
              className={({ isActive }) =>
                `flex items-center gap-3 px-3 py-2 rounded-lg text-sm font-medium transition-colors ${
                  isActive ? 'bg-sky-500/10 text-sky-400' : 'text-gray-400 hover:text-gray-200 hover:bg-gray-800'
                }`
              }
            >
              <item.icon size={18} />
              {sidebarOpen && item.label}
            </NavLink>
          ))}
        </nav>
        
        <div className="p-4 border-t border-gray-800">
          <button
            onClick={() => {
              localStorage.removeItem('router_api_key')
              window.location.reload()
            }}
            className="flex items-center gap-3 px-3 py-2 w-full rounded-lg text-sm font-medium text-gray-400 hover:text-red-400 hover:bg-red-950/30 transition-colors"
          >
            <LogOut size={18} />
            Logout
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

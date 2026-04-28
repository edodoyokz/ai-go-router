import React, { forwardRef } from 'react'
import clsx from 'clsx'

export const Select = forwardRef(function Select(
  { label, error, className, fullWidth, helpText, icon: Icon, children, ...props },
  ref
) {
  return (
    <div className={clsx('flex flex-col gap-1.5', fullWidth && 'w-full', className)}>
      {label && <label className="text-sm font-medium text-gray-300">{label}</label>}
      <div className="relative flex items-center">
        {Icon && (
          <div className="absolute left-3 text-gray-500 pointer-events-none">
            <Icon size={16} />
          </div>
        )}
        <select
          ref={ref}
          className={clsx(
            'flex h-10 w-full appearance-none rounded-md border border-gray-700 bg-gray-900 px-3 py-2 text-sm text-white transition-colors focus:outline-none focus:ring-2 focus:ring-sky-500/50 disabled:cursor-not-allowed disabled:opacity-50',
            Icon && 'pl-9',
            error && 'border-red-500 focus:ring-red-500/50'
          )}
          {...props}
        >
          {children}
        </select>
        <div className="absolute inset-y-0 right-0 flex items-center pr-3 pointer-events-none text-gray-500">
          <svg className="w-4 h-4" fill="none" stroke="currentColor" viewBox="0 0 24 24">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth="2" d="M19 9l-7 7-7-7" />
          </svg>
        </div>
      </div>
      {error && <span className="text-xs text-red-400">{error}</span>}
      {helpText && !error && <span className="text-xs text-gray-500">{helpText}</span>}
    </div>
  )
})

import React, { forwardRef } from 'react'
import clsx from 'clsx'

export const Input = forwardRef(function Input(
  { type = 'text', label, error, className, fullWidth, helpText, icon: Icon, ...props },
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
        <input
          ref={ref}
          type={type}
          className={clsx(
            'flex h-10 w-full rounded-md border border-gray-700 bg-gray-900 px-3 py-2 text-sm placeholder:text-gray-500 text-white transition-colors focus:outline-none focus:ring-2 focus:ring-sky-500/50 disabled:cursor-not-allowed disabled:opacity-50',
            Icon && 'pl-9',
            error && 'border-red-500 focus:ring-red-500/50'
          )}
          {...props}
        />
      </div>
      {error && <span className="text-xs text-red-400">{error}</span>}
      {helpText && !error && <span className="text-xs text-gray-500">{helpText}</span>}
    </div>
  )
})

import React from 'react'
import clsx from 'clsx'

export function Toggle({ label, description, checked, onChange, disabled }) {
  return (
    <label className={clsx(
      'flex items-center gap-4 cursor-pointer',
      disabled && 'opacity-50 cursor-not-allowed'
    )}>
      <div className="relative">
        <input
          type="checkbox"
          className="sr-only"
          checked={checked}
          onChange={(e) => onChange(e.target.checked)}
          disabled={disabled}
        />
        <div className={clsx(
          'block w-10 h-6 rounded-full transition-colors',
          checked ? 'bg-sky-500' : 'bg-gray-700'
        )} />
        <div className={clsx(
          'absolute left-1 top-1 bg-white w-4 h-4 rounded-full transition-transform',
          checked ? 'translate-x-4' : 'translate-x-0'
        )} />
      </div>
      {(label || description) && (
        <div className="flex flex-col">
          {label && <span className="text-sm font-medium text-gray-200">{label}</span>}
          {description && <span className="text-xs text-gray-500">{description}</span>}
        </div>
      )}
    </label>
  )
}

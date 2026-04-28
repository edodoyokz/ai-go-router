import React from 'react'
import clsx from 'clsx'

const variants = {
  success: 'bg-green-500/10 text-green-400 border border-green-500/20',
  error: 'bg-red-500/10 text-red-400 border border-red-500/20',
  warning: 'bg-yellow-500/10 text-yellow-400 border border-yellow-500/20',
  info: 'bg-sky-500/10 text-sky-400 border border-sky-500/20',
  neutral: 'bg-gray-800 text-gray-300 border border-gray-700',
}

const sizes = {
  sm: 'px-1.5 py-0.5 text-[10px]',
  md: 'px-2.5 py-0.5 text-xs',
  lg: 'px-3 py-1 text-sm',
}

export function Badge({ variant = 'neutral', size = 'md', className, dot, children }) {
  return (
    <span className={clsx(
      'inline-flex items-center gap-1.5 font-medium rounded-full',
      variants[variant],
      sizes[size],
      className
    )}>
      {dot && (
        <span className={clsx(
          'w-1.5 h-1.5 rounded-full',
          variant === 'success' && 'bg-green-400',
          variant === 'error' && 'bg-red-400',
          variant === 'warning' && 'bg-yellow-400',
          variant === 'info' && 'bg-sky-400',
          variant === 'neutral' && 'bg-gray-400'
        )} />
      )}
      {children}
    </span>
  )
}

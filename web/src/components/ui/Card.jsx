import React from 'react'
import clsx from 'clsx'

export function Card({ className, children, hoverable, onClick }) {
  return (
    <div
      className={clsx(
        'bg-gray-900 border border-gray-800 rounded-xl overflow-hidden',
        hoverable && 'hover:border-gray-700 transition-colors cursor-pointer',
        className
      )}
      onClick={onClick}
    >
      {children}
    </div>
  )
}

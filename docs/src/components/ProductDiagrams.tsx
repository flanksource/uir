'use client'

import { useEffect, useState, type ReactNode } from 'react'
import { IntoUIR, OutOfUIR } from '../../uir-how-it-works'
import './diagrams.css'

export function ProductDiagram({ kind, caption }: { kind: 'inputs' | 'outputs'; caption: ReactNode }) {
  const [mounted, setMounted] = useState(false)
  useEffect(() => setMounted(true), [])
  const Diagram = kind === 'inputs' ? IntoUIR : OutOfUIR
  const label = kind === 'inputs' ? 'Lowering and publication diagram' : 'Queries, analyses, and storage diagram'

  return (
    <figure className="relative left-1/2 my-6 w-[min(80rem,calc(100vw-3rem))] -translate-x-1/2">
      <div className="overflow-x-auto rounded-xl bg-[#f4f7fc] scheme-light [scrollbar-width:thin] [&_pre]:m-0 [&_pre]:whitespace-pre"
        tabIndex={0} role="region" aria-label={label}>
        <div className="w-max min-h-[210px] leading-normal">
          {mounted && <Diagram />}
        </div>
      </div>
      <figcaption className="mt-3 text-sm text-[var(--vocs-text-color-secondary)]">{caption}</figcaption>
    </figure>
  )
}

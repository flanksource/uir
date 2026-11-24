'use client'

import { useEffect, useState } from 'react'
import {
  ModulesAndPublication, FilesAndDocuments, SymbolLocations, SymbolReferences,
  SnapshotMembership, SymbolAllocation, SymbolDeltas,
} from '../../uir-gorm-erd'
import './diagrams.css'

const diagrams = {
  modules: ModulesAndPublication,
  documents: FilesAndDocuments,
  locations: SymbolLocations,
  references: SymbolReferences,
  membership: SnapshotMembership,
  allocation: SymbolAllocation,
  deltas: SymbolDeltas,
}

export function StorageERD({ section, label }: { section: keyof typeof diagrams; label: string }) {
  const [mounted, setMounted] = useState(false)
  useEffect(() => setMounted(true), [])
  const Content = diagrams[section]
  if (!Content) throw new Error(`Unknown storage ERD section: ${section}`)
  return (
    <figure className="mx-auto my-6 w-fit max-w-full" aria-label={label}>
      <div className="overflow-x-auto rounded-xl bg-[#f7fbfe] [scrollbar-width:thin]" tabIndex={0} role="region" aria-label={label}>
        <div className="w-max min-h-[210px] px-6 leading-normal">{mounted && <Content />}</div>
      </div>
    </figure>
  )
}

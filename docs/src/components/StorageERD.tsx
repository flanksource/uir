'use client'

import { useEffect, useState } from 'react'
import { COLORS } from '@flanksource/facet/diagram/colors'
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
    <figure className="uir-diagram uir-erd" aria-label={label}>
      <div className="uir-diagram-scroll" tabIndex={0} role="region" aria-label={label}
        style={{ backgroundColor: COLORS.background }}>
        <div className="uir-erd-canvas">{mounted && <Content />}</div>
      </div>
    </figure>
  )
}

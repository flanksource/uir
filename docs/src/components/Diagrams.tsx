'use client'

import { useEffect, useState, type ComponentProps } from 'react'
import Diagram from '@flanksource/facet/diagram/Diagram'
import BoxNode from '@flanksource/facet/diagram/BoxNode'
import Arrow from '@flanksource/facet/diagram/Arrow'
import { COLORS } from '@flanksource/facet/diagram/colors'
import './diagrams.css'

function BrowserDiagram({ children }: Pick<ComponentProps<typeof Diagram>, 'children'>) {
  const [mounted, setMounted] = useState(false)
  useEffect(() => setMounted(true), [])
  return mounted
    ? <Diagram className="uir-diagram-canvas">{children}</Diagram>
    : <div className="uir-diagram-canvas" />
}

function Stage({ id, title, items, color = COLORS.primary }: {
  id: string
  title: string
  items: string[]
  color?: string
}) {
  return (
    <BoxNode id={id} title={<span className="text-base">{title}</span>}
      headerColor={color} borderColor={color} bodyColor={COLORS.background} minWidth="210px">
      <div className="w-[182px] space-y-3 text-sm" style={{ color: COLORS.accent }}>
        {items.map((item) => <p key={item}>{item}</p>)}
      </div>
    </BoxNode>
  )
}

export function OverviewDiagram() {
  return (
    <figure className="uir-diagram" aria-label="UIR architecture: producers, model, storage, and consumers">
      <div className="uir-diagram-scroll" tabIndex={0} role="region" aria-label="UIR architecture diagram"
        style={{ backgroundColor: COLORS.background }}>
        <BrowserDiagram>
          {(id) => (
            <>
              <div className="flex items-center gap-10 p-6">
                <Stage id={id('producers')} title="Producers"
                  items={['Go source indexing', 'External publications', 'Language lowering']} />
                <Stage id={id('model')} title="UIR model"
                  items={['Nodes and symbols', 'Expressions / statements', 'Symbol relationships']} />
                <Stage id={id('snapshots')} title="Snapshot storage"
                  items={['SQLite or PostgreSQL', 'Immutable snapshots', 'Documents and deltas']} />
                <Stage id={id('consumers')} title="Consumers" color={COLORS.outputBorder}
                  items={['Queries and call graphs', 'Structural symbol diffs', 'Browser and HTTP API']} />
              </div>
              <Arrow variant="primary" from={id('producers')} to={id('model')}
                path="straight" startAnchor="right" endAnchor="left" />
              <Arrow variant="primary" from={id('model')} to={id('snapshots')}
                path="straight" startAnchor="right" endAnchor="left" />
              <Arrow variant="primary" from={id('snapshots')} to={id('consumers')}
                path="straight" startAnchor="right" endAnchor="left" />
            </>
          )}
        </BrowserDiagram>
      </div>
      <figcaption>Extractors produce UIR facts. Snapshot storage makes those facts available to queries, graphs, diffs, and the browser. The model can also be used directly as a library.</figcaption>
    </figure>
  )
}

export function PublicationDiagram() {
  return (
    <figure className="uir-diagram" aria-label="Go indexing and atomic snapshot publication">
      <div className="uir-diagram-scroll" tabIndex={0} role="region" aria-label="Snapshot publication diagram"
        style={{ backgroundColor: COLORS.background }}>
        <BrowserDiagram>
          {(id) => (
            <>
              <div className="flex items-center gap-8 p-6">
                <Stage id={id('prepare')} title="Prepare inputs" color={COLORS.muted}
                  items={['Discover modules', 'Hash module inputs', 'Extract package facts']} />
                <div className="relative flex items-center gap-8 rounded-xl border-2 border-solid px-4 py-6"
                  style={{ borderColor: COLORS.primary }}>
                  <div className="absolute -top-3 left-6 px-2 text-sm font-bold"
                    style={{ backgroundColor: COLORS.background, color: COLORS.accent }}>
                    One publication transaction
                  </div>
                  <Stage id={id('facts')} title="Publish snapshot"
                    items={['Revisions and documents', 'Deltas and coverage', 'Dependency links and stats']} />
                  <Stage id={id('head')} title="Advance location head"
                    items={['Root + checkout location', 'Compare-and-swap version', 'Commit together']} />
                </div>
                <Stage id={id('readers')} title="Read saved state" color={COLORS.outputBorder}
                  items={['Select head or snapshot', 'Resolve effective sources', 'Load active documents']} />
              </div>
              <Arrow variant="primary" from={id('prepare')} to={id('facts')}
                path="straight" startAnchor="right" endAnchor="left" />
              <Arrow variant="primary" from={id('facts')} to={id('head')}
                path="straight" startAnchor="right" endAnchor="left" />
              <Arrow variant="primary" from={id('head')} to={id('readers')}
                path="straight" startAnchor="right" endAnchor="left" />
            </>
          )}
        </BrowserDiagram>
      </div>
      <figcaption>For a changed Go module, extraction finishes before publication starts. Snapshot facts and the version-checked location head commit together. Readers resolve sources and package input hashes to load the saved documents.</figcaption>
    </figure>
  )
}

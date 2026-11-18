// @live
import React from 'react';
import Arrow from '@flanksource/facet/diagram/Arrow';
import { COLORS } from '@flanksource/facet/diagram/colors';
import { NodePill } from '@flanksource/facet/diagram/content';
import Diagram from '@flanksource/facet/diagram/Diagram';
import Page from '@flanksource/facet/Page';
import Section from '@flanksource/facet/Section';
import { GapList, GapMarker, Legend, StatusBox, type Gap } from './uir-diagram-gaps';
import { Entity, Label } from './uir-gorm-erd';
import { Block, Caption, Panel, mono } from './uir-how-it-works';

type Operation = 'set' | 'delete';

const OPERATION_COLORS: Readonly<Record<Operation, string>> = { set: COLORS.outputBorder, delete: COLORS.muted };

function Op({ op }: { op: Operation }) {
  return <span className="font-bold" style={{ color: OPERATION_COLORS[op] }}>{op}</span>;
}

function Heading({ children }: { children: React.ReactNode }) {
  return <div className="mb-1 text-[8pt] font-bold uppercase tracking-wide" style={{ color: COLORS.muted }}>{children}</div>;
}

function Code({ children }: { children: React.ReactNode }) {
  return <code className="font-mono text-[8.5pt]" style={{ color: COLORS.accent }}>{children}</code>;
}

/* Fingerprints are public · private · content, the values storage/symbol_deltas_integration_test.go writes. */
type SymbolRow = { op: Operation; name: string; kind: string; fingerprints?: string };

function SymbolRows({ title, rows, operations = true }: { title: string; rows: SymbolRow[]; operations?: boolean }) {
  return <Panel>
    <Heading>{title}</Heading>
    {rows.map((row) => <div key={`${row.op}-${row.name}`} className={`grid gap-x-1.5 ${mono}`}
      style={{ gridTemplateColumns: operations ? '48px 1fr auto' : '1fr auto' }}>
      {operations && <Op op={row.op} />}
      <span style={{ color: COLORS.accent }}>{row.name}</span>
      <span style={{ color: COLORS.muted }}>{row.fingerprints ?? '—'}</span>
    </div>)}
  </Panel>;
}

function Effective({ rows }: { rows: SymbolRow[] }) {
  return <SymbolRows title="EffectiveSymbols resolves" rows={rows} operations={false} />;
}

function KeyRows({ rows }: { rows: Array<[string, React.ReactNode]> }) {
  return <div className="flex flex-col gap-0.5">
    {rows.map(([label, value]) => <div key={label} className="grid grid-cols-[52px_1fr] gap-x-1.5 text-[8.5pt] leading-snug">
      <span className="font-semibold" style={{ color: COLORS.muted }}>{label}</span>
      <span style={{ color: COLORS.accent }}>{value}</span>
    </div>)}
  </div>;
}

const STORE = { name: 'Store', kind: 'type' };
const SAVE = { name: 'Save', kind: 'method' };
const LOAD = { name: 'load', kind: 'func' };
const RUN = { name: 'Run', kind: 'func' };

export function DeltaChain() {
  return <Diagram className="relative">{(id) => <>
    <div className="grid px-6 py-8" style={{ gridTemplateColumns: 'repeat(4, 220px)', columnGap: '64px', rowGap: '12px', justifyContent: 'center' }}>
      <Block id={id('initial')} title="initial" width="220px">
        <div className={mono} style={{ color: COLORS.muted }}>no base</div>
      </Block>
      <Block id={id('edited')} title="edited" width="220px">
        <div className={mono} style={{ color: COLORS.muted }}>base_snapshot_id → initial</div>
      </Block>
      <Block id={id('added')} title="added" width="220px">
        <div className={mono} style={{ color: COLORS.muted }}>base_snapshot_id → edited</div>
      </Block>
      <Block id={id('readded')} title="readded" width="220px">
        <div className={mono} style={{ color: COLORS.muted }}>base_snapshot_id → added</div>
      </Block>
      <SymbolRows title="symbol_deltas rows it stores" rows={[
        { op: 'set', ...STORE, fingerprints: '1 · 101 · 1' },
        { op: 'set', ...SAVE, fingerprints: '2 · 102 · 2' },
        { op: 'set', ...LOAD, fingerprints: '3 · 103 · 3' },
      ]} />
      <SymbolRows title="symbol_deltas rows it stores" rows={[
        { op: 'set', ...SAVE, fingerprints: '2 · 102 · 5' },
        { op: 'delete', ...LOAD },
      ]} />
      <SymbolRows title="symbol_deltas rows it stores" rows={[
        { op: 'set', ...RUN, fingerprints: '4 · 104 · 4' },
      ]} />
      <SymbolRows title="symbol_deltas rows it stores" rows={[
        { op: 'delete', ...STORE },
        { op: 'set', ...LOAD, fingerprints: '3 · 103 · 9' },
      ]} />
      <Effective rows={[
        { op: 'set', ...STORE, fingerprints: '1 · 101 · 1' },
        { op: 'set', ...SAVE, fingerprints: '2 · 102 · 2' },
        { op: 'set', ...LOAD, fingerprints: '3 · 103 · 3' },
      ]} />
      <Effective rows={[
        { op: 'set', ...STORE, fingerprints: '1 · 101 · 1' },
        { op: 'set', ...SAVE, fingerprints: '2 · 102 · 5' },
      ]} />
      <Effective rows={[
        { op: 'set', ...STORE, fingerprints: '1 · 101 · 1' },
        { op: 'set', ...SAVE, fingerprints: '2 · 102 · 5' },
        { op: 'set', ...RUN, fingerprints: '4 · 104 · 4' },
      ]} />
      <Effective rows={[
        { op: 'set', ...SAVE, fingerprints: '2 · 102 · 5' },
        { op: 'set', ...LOAD, fingerprints: '3 · 103 · 9' },
        { op: 'set', ...RUN, fingerprints: '4 · 104 · 4' },
      ]} />
      <div style={{ gridColumn: '1 / span 4' }}>
        <Caption>
          Each snapshot stores only how its defined symbols differ from its base: <Op op="set" /> with the public · private · content fingerprints, or <Op op="delete" /> with none. <Code>EffectiveSymbols</Code> walks the base chain in one <Code>WITH RECURSIVE</Code> query, keeps the newest row per <Code>symbol_handle</Code> and drops deletes, however deep the chain; a chain past 100,000 links is an error. Data from <Code>storage/symbol_deltas_integration_test.go</Code>: <Code>Store</Code> is a type, <Code>Save</Code> a method, <Code>load</Code> and <Code>Run</Code> funcs.
        </Caption>
      </div>
    </div>
    <Arrow variant="secondary" from={id('edited')} to={id('initial')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="secondary" from={id('added')} to={id('edited')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="secondary" from={id('readded')} to={id('added')} path="straight" startAnchor="left" endAnchor="right" />
  </>}</Diagram>;
}

export function FourDeltas() {
  return <Diagram className="relative">{(id) => <>
    <div className="grid px-6 py-8" style={{ gridTemplateColumns: '290px 220px 290px', columnGap: '160px', rowGap: '40px', justifyContent: 'center', alignItems: 'center' }}>
      <div style={{ gridColumn: 1, gridRow: 1 }}>
        <Block id={id('source')} title="source_deltas" command="EffectiveSources" width="290px">
          <KeyRows rows={[
            ['key', <Code>snapshot_id, path_key</Code>],
            ['set', <><Code>revision_id</Code> → <Code>source_revisions</Code></>],
            ['delete', <><Code>revision_id</Code> is null</>],
          ]} />
        </Block>
      </div>
      <div style={{ gridColumn: 1, gridRow: 2 }}>
        <Block id={id('symbol')} title="symbol_deltas" command="EffectiveSymbols" width="290px">
          <KeyRows rows={[
            ['key', <Code>snapshot_ordinal, symbol_handle</Code>],
            ['set', <><Code>public_fp</Code>, <Code>private_fp</Code>, <Code>content_fp</Code></>],
            ['delete', 'no fingerprints'],
          ]} />
        </Block>
      </div>
      <div style={{ gridColumn: 2, gridRow: '1 / span 2' }}>
        <Block id={id('snapshots')} title="snapshots" width="220px">
          <KeyRows rows={[
            ['id', <Code>uuid</Code>],
            ['ordinal', <Code>bigint, unique</Code>],
            ['base', <Code>base_snapshot_id</Code>],
          ]} />
          <Caption>A row exists only once its publication committed. The base is a storage base, not Git parentage.</Caption>
        </Block>
      </div>
      <div style={{ gridColumn: 3, gridRow: 1 }}>
        <Block id={id('document')} title="document_deltas" command="ActiveDocuments" width="290px">
          <KeyRows rows={[
            ['key', <Code>snapshot_ordinal, path_key</Code>],
            ['set', <><Code>document_ordinal</Code> → <Code>documents</Code></>],
            ['delete', <><Code>document_ordinal</Code> is null</>],
          ]} />
        </Block>
      </div>
      <div style={{ gridColumn: 3, gridRow: 2 }}>
        <Block id={id('implements')} title="implements_deltas" command="EffectiveImplements" width="290px">
          <KeyRows rows={[
            ['key', <Code>snapshot_ordinal, type_handle, interface_handle</Code>],
            ['set', 'the type satisfies the interface'],
            ['delete', 'the base held the pair; this snapshot does not'],
          ]} />
        </Block>
      </div>
      <div style={{ gridColumn: 2, gridRow: 3, justifySelf: 'center' }}>
        <Block id={id('coverage')} title="package_coverage: full copy" color={COLORS.muted} width="290px" dashed>
          <KeyRows rows={[
            ['key', <Code>snapshot_id, package_path</Code>],
            ['rows', 'one per package in every snapshot, not a delta'],
            ['holds', <><Code>input_hash</Code>, <Code>export_shape_hash</Code>, <Code>coverage</Code>, <Code>file_count</Code>, <Code>diagnostics</Code></>],
          ]} />
        </Block>
      </div>
      <div style={{ gridColumn: '1 / span 3' }}>
        <Caption>
          Every delta row points at an immutable, shared fact: a <Code>source_revisions</Code> row, a <Code>documents</Code> row, or a <Code>symbols.handle</Code> (<Code>implements_deltas</Code> names two). Snapshots never copy those facts. Each reader folds its table along the base chain with the same recursive query, newest row per key first. Deleting a snapshot cascades to all five tables.
        </Caption>
      </div>
    </div>
    <Arrow variant="er" from={id('source')} to={id('snapshots')} path="straight" startAnchor="right" endAnchor={{ position: 'left', offset: { y: -50 } }}
      labels={{ middle: <Label>root_id, snapshot_id</Label> }} />
    <Arrow variant="er" from={id('symbol')} to={id('snapshots')} path="straight" startAnchor="right" endAnchor={{ position: 'left', offset: { y: 50 } }}
      labels={{ middle: <Label>snapshot_ordinal</Label> }} />
    <Arrow variant="er" from={id('document')} to={id('snapshots')} path="straight" startAnchor="left" endAnchor={{ position: 'right', offset: { y: -50 } }}
      labels={{ middle: <Label>root_id, snapshot_ordinal</Label> }} />
    <Arrow variant="er" from={id('implements')} to={id('snapshots')} path="straight" startAnchor="left" endAnchor={{ position: 'right', offset: { y: 50 } }}
      labels={{ middle: <Label>snapshot_ordinal</Label> }} />
    <Arrow variant="er" from={id('coverage')} to={id('snapshots')} path="straight" startAnchor="top" endAnchor="bottom"
      labels={{ middle: <Label>root_id, snapshot_id</Label> }} />
  </>}</Diagram>;
}

function Step({ children }: { children: React.ReactNode }) {
  return <div className="text-[8.5pt] leading-snug" style={{ color: COLORS.accent }}>{children}</div>;
}

type EditResult = { edit: React.ReactNode; rows: React.ReactNode };

const EDIT_RESULTS: EditResult[] = [
  { edit: 'First index, no base', rows: <><Op op="set" /> for every defined symbol</> },
  { edit: <>Body edit of <Code>Add</Code></>, rows: <><Op op="set" /> <Code>Add</Code>: same public and private, new content</> },
  { edit: <>Signature edit: <Code>Unit()</Code> returns <Code>*Amount</Code></>, rows: <><Op op="set" /> <Code>Unit</Code>: new public. <Code>book</Code> is re-extracted, <Code>book.go</Code> keeps its document, no rows for its symbols</> },
  { edit: <>Add <Code>func Half</Code></>, rows: <><Op op="set" /> <Code>Half</Code></> },
  { edit: <>Remove <Code>func Scale</Code></>, rows: <><Op op="delete" /> <Code>Scale</Code>, all three fingerprints null</> },
  { edit: <>Move <Code>Count</Code> from <Code>book.go</Code> to a new <Code>count.go</Code></>, rows: 'none: the same fingerprints on both sides' },
  { edit: <>Delete <Code>book/use.go</Code></>, rows: <><Op op="delete" /> <Code>Twice</Code></> },
  { edit: <><Code>go.sum</Code> bump v1.0.0 → v1.1.0</>, rows: 'none, though the snapshot still has a base' },
];

export function PublishDeltas() {
  return <Diagram className="relative">{(id) => <>
    <div className="flex items-start justify-center px-6 py-8" style={{ columnGap: '80px' }}>
      <div className="flex flex-col" style={{ rowGap: '36px' }}>
        <Block id={id('edit')} title="1 · Edit money/money.go" width="320px">
          <div className={mono}>
            <div style={{ color: COLORS.muted }}>- Cents: a.Cents + b.Cents,</div>
            <div style={{ color: COLORS.accent }}>+ Cents: b.Cents + a.Cents,</div>
          </div>
          <Step>Only the body of <Code>Add</Code> changes; its signature does not.</Step>
        </Block>
        <Block id={id('revision')} title="2 · New source revision" width="320px">
          <Step>A new <Code>content_hash</Code> gives a new <Code>source_revisions</Code> row, and <Code>source_deltas</Code> <Op op="set" /> <Code>money/money.go</Code>.</Step>
        </Block>
        <Block id={id('input')} title="3 · Package input hash" width="320px">
          <Step>The <Code>money</Code> package's input hash differs from its base <Code>package_coverage</Code> row, so its files are rendered again. <Code>book</Code> imports <Code>money</Code>, but the export shape is unchanged, so <Code>book</Code> keeps its input hash and its documents.</Step>
        </Block>
        <Block id={id('document')} title="4 · Document digest" width="320px">
          <Step><Code>money.go</Code> renders differently: a new <Code>documents</Code> row, and <Code>document_deltas</Code> <Op op="set" /> <Code>money/money.go</Code>. A file that renders the same keeps its document.</Step>
        </Block>
        <Block id={id('symbols')} title="5 · Symbol deltas" color={COLORS.outputBorder} width="320px">
          <Step><Code>createSymbolDeltas</Code> compares the whole <Code>money</Code> package, base documents against the extraction: <Op op="set" /> <Code>Add</Code> with the same <Code>public_fp</Code> and <Code>private_fp</Code> and a new <Code>content_fp</Code>.</Step>
        </Block>
      </div>
      <Block id={id('results')} title="What each edit writes to symbol_deltas" color={COLORS.outputBorder} width="520px">
        <div className="flex flex-col">
          {EDIT_RESULTS.map((result, index) => <div key={index} className="grid grid-cols-[190px_1fr] gap-x-3 border-t py-1.5 text-[8.5pt] leading-snug first:border-t-0"
            style={{ borderColor: COLORS.primary }}>
            <span style={{ color: COLORS.muted }}>{result.edit}</span>
            <span style={{ color: COLORS.accent }}>{result.rows}</span>
          </div>)}
        </div>
        <Caption>The first seven cases are successive publications of one module; the <Code>go.sum</Code> bump is a separate two-publication case on another module. Both are asserted on SQLite and PostgreSQL in <Code>indexer/symbol_deltas_integration_test.go</Code>. Only packages with a path whose bound document changed are compared.</Caption>
      </Block>
    </div>
    <div className="mx-auto px-6 pb-6" style={{ maxWidth: '750pt' }}>
      <Caption>
        One transaction, in this order (<Code>indexer/publication.go</Code>): <Code>source_revisions</Code>, <Code>symbols</Code>, <Code>documents</Code> with their <Code>symbol_postings</Code> and <Code>declaration_metrics</Code>; the <Code>snapshots</Code> row; <Code>source_blobs</Code> (only when the snapshot is not a clean commit or has an analysis mode, and only for files without a Git path) and the snapshot's dependency edges; <Code>source_deltas</Code>; <Code>document_deltas</Code>; <Code>package_coverage</Code>, in full, with the sibling shapes; <Code>symbol_deltas</Code>; <Code>implements_deltas</Code>; snapshot stats; then the <Code>location_heads</Code> compare-and-swap, which a historical publication skips, leaving the head unchanged.
      </Caption>
    </div>
    <Arrow variant="primary" from={id('edit')} to={id('revision')} path="straight" startAnchor="bottom" endAnchor="top" />
    <Arrow variant="primary" from={id('revision')} to={id('input')} path="straight" startAnchor="bottom" endAnchor="top" />
    <Arrow variant="primary" from={id('input')} to={id('document')} path="straight" startAnchor="bottom" endAnchor="top" />
    <Arrow variant="primary" from={id('document')} to={id('symbols')} path="straight" startAnchor="bottom" endAnchor="top" />
  </>}</Diagram>;
}

function LaneLabel({ title, children }: { title: string; children: React.ReactNode }) {
  return <div className="flex flex-col gap-1">
    <span className="text-[9.5pt] font-bold" style={{ color: COLORS.accent }}>{title}</span>
    <span className="text-[8.5pt] leading-snug" style={{ color: COLORS.muted }}>{children}</span>
  </div>;
}

function SourceRows({ rows }: { rows: string[] }) {
  return <div className={mono}>
    {rows.map((row) => <div key={row}><Op op="set" /> <span style={{ color: COLORS.accent }}>{row}</span></div>)}
  </div>;
}

export function BaseSelection() {
  return <Diagram className="relative">{(id) => <>
    <div className="grid px-6 py-8" style={{ gridTemplateColumns: '200px repeat(3, 210px)', columnGap: '64px', rowGap: '14px', justifyContent: 'center', alignItems: 'center' }}>
      <LaneLabel title="Head publication">
        <Code>loadModuleBase</Code>: the location's own head; a checkout without one starts on the primary location's head.
      </LaneLabel>
      <Block id={id('main-1')} title="main-1" width="210px">
        <div className={`mb-1 ${mono}`} style={{ color: COLORS.muted }}>/workspace/service</div>
        <SourceRows rows={['alpha.go', 'beta.go v1']} />
      </Block>
      <Block id={id('main-2')} title="main-2 · head" width="210px">
        <div className={`mb-1 ${mono}`} style={{ color: COLORS.muted }}>/workspace/service</div>
        <SourceRows rows={['beta.go v2']} />
      </Block>
      <Block id={id('feature')} title="feature-1" width="210px">
        <div className={`mb-1 ${mono}`} style={{ color: COLORS.muted }}>/workspace/service-feature</div>
        <SourceRows rows={['gamma.go']} />
      </Block>
      <div style={{ gridColumn: '2 / span 3' }}>
        <Caption>
          <Code>EffectiveSources(feature-1)</Code> is <Code>alpha.go</Code>, <Code>beta.go v2</Code>, <Code>gamma.go</Code>; <Code>EffectiveSources(main-2)</Code> stays <Code>alpha.go</Code>, <Code>beta.go v2</Code>. The branch's file never reaches the head it bases on. Those results are what <Code>storage/effective_sources_integration_test.go</Code> asserts; the test wires feature-1's base by hand, so the base choice shown is <Code>loadModuleBase</Code>'s rule (<Code>indexer/modules_locations.go:39</Code>), not something it exercises.
        </Caption>
      </div>
      <div style={{ gridColumn: '1 / span 4', height: '15pt' }} />
      <LaneLabel title="Historical revision">
        <Code>revisionBase</Code>: the newest clean snapshot of the nearest indexed ancestor, up to 1,024 commits back, with the same configuration and dependency set; else the head. The head never moves.
      </LaneLabel>
      <Block id={id('third')} title="third · head" width="210px">
        <div className="text-[8.5pt] leading-snug" style={{ color: COLORS.muted }}>"test: add a file", indexed first, as the checkout's head.</div>
      </Block>
      <Block id={id('first')} title="first · historical" width="210px">
        <div className="text-[8.5pt] leading-snug" style={{ color: COLORS.muted }}><Code>revisionBase</Code> finds no indexed ancestor and falls back to the head.</div>
      </Block>
      <Block id={id('second')} title="second · historical" width="210px">
        <div className="mb-1 text-[8.5pt] leading-snug" style={{ color: COLORS.muted }}>"test: change a body"; base is first, its parent.</div>
        <SourceRows rows={['tax/rate.go']} />
        <div className={mono} style={{ color: COLORS.accent }}>1 symbol delta: <span style={{ color: COLORS.muted }}>Rate</span></div>
      </Block>
      <div style={{ gridColumn: '2 / span 3' }}>
        <Caption>
          <Code>indexer/revision_reuse_integration_test.go</Code> asserts second's base is first, its one source delta (<Code>tax/rate.go</Code>), its one symbol delta, and that only <Code>tax</Code>'s two files are rendered. first's fallback to the head follows from <Code>revisionBase</Code> (<Code>indexer/revision_base.go:36</Code>) and is not asserted.
        </Caption>
      </div>
    </div>
    <Arrow variant="secondary" from={id('main-2')} to={id('main-1')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="secondary" from={id('feature')} to={id('main-2')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="secondary" from={id('first')} to={id('third')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="secondary" from={id('second')} to={id('first')} path="straight" startAnchor="left" endAnchor="right" />
  </>}</Diagram>;
}

export function PruneRebase() {
  return <Diagram className="relative">{(id) => <>
    <div className="grid px-6 py-8" style={{ gridTemplateColumns: 'repeat(4, 220px)', columnGap: '64px', rowGap: '14px', justifyContent: 'center', alignItems: 'center' }}>
      <div style={{ gridColumn: '1 / span 4' }}><Heading>Before uir prune --keep 2</Heading></div>
      <Block id={id('initial')} title="initial · deleted" color={COLORS.muted} width="220px">
        <SymbolRows title="symbol_deltas" rows={[
          { op: 'set', ...STORE, fingerprints: '1 · 101 · 1' },
          { op: 'set', ...SAVE, fingerprints: '2 · 102 · 2' },
          { op: 'set', ...LOAD, fingerprints: '3 · 103 · 3' },
        ]} />
      </Block>
      <Block id={id('edited')} title="edited · deleted" color={COLORS.muted} width="220px">
        <SymbolRows title="symbol_deltas" rows={[
          { op: 'set', ...SAVE, fingerprints: '2 · 102 · 5' },
          { op: 'delete', ...LOAD },
        ]} />
      </Block>
      <Block id={id('added')} title="added · kept" width="220px">
        <SymbolRows title="symbol_deltas" rows={[
          { op: 'set', ...RUN, fingerprints: '4 · 104 · 4' },
        ]} />
      </Block>
      <Block id={id('readded')} title="readded · kept" width="220px">
        <SymbolRows title="symbol_deltas" rows={[
          { op: 'delete', ...STORE },
          { op: 'set', ...LOAD, fingerprints: '3 · 103 · 9' },
        ]} />
      </Block>
      <div style={{ gridColumn: '1 / span 4', height: '27pt' }} />
      <div style={{ gridColumn: '1 / span 2', alignSelf: 'center' }}>
        <Heading>After</Heading>
        <Caption>
          added's base is deleted, so added is rebased: its own rows are replaced by one <Op op="set" /> per symbol it resolves, and likewise for its sources, document bindings and implements facts. Its <Code>base_snapshot_id</Code> becomes null; its recorded change stats stay as published. readded still bases on it and is untouched.
        </Caption>
      </div>
      <Block id={id('added-after')} title="added · rebased, no base" color={COLORS.outputBorder} width="220px">
        <SymbolRows title="symbol_deltas" rows={[
          { op: 'set', ...STORE, fingerprints: '1 · 101 · 1' },
          { op: 'set', ...SAVE, fingerprints: '2 · 102 · 5' },
          { op: 'set', ...RUN, fingerprints: '4 · 104 · 4' },
        ]} />
      </Block>
      <Block id={id('readded-after')} title="readded · kept" width="220px">
        <SymbolRows title="symbol_deltas" rows={[
          { op: 'delete', ...STORE },
          { op: 'set', ...LOAD, fingerprints: '3 · 103 · 9' },
        ]} />
      </Block>
      <div style={{ gridColumn: '1 / span 4' }}>
        <Caption>
          The chain from the first diagram. Rebasing writes no tombstones, so <Code>load</Code>'s delete in edited is gone. Both kept snapshots resolve exactly what they resolved before. <Code>storage/prune_apply.go</Code> (<Code>rebaseSnapshot</Code>), then deleted snapshots cascade their deltas, and unused documents, revisions, blobs and orphan symbols go.
        </Caption>
      </div>
    </div>
    <Arrow variant="secondary" from={id('edited')} to={id('initial')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="secondary" from={id('added')} to={id('edited')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="secondary" from={id('readded')} to={id('added')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="primary" from={id('added')} to={id('added-after')} path="straight" startAnchor="bottom" endAnchor="top"
      labels={{ middle: <NodePill>rebased</NodePill> }} />
    <Arrow variant="secondary" from={id('readded-after')} to={id('added-after')} path="straight" startAnchor="left" endAnchor="right" />
  </>}</Diagram>;
}

export function DeltaTables() {
  return <Diagram className="relative py-8">{(id) => <>
    <div className="flex items-start justify-center" style={{ columnGap: '170px' }}>
      <Entity id={id('source')} title="source_deltas" detail="05_source_deltas.hcl" accent={COLORS.fk} fields={[
        { name: 'snapshot_id', type: 'uuid', pk: true, fk: true },
        { name: 'path_key', type: 'text', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'revision_id', type: 'uuid? · set only', fk: true },
        { name: 'operation', type: 'set | delete' },
      ]} indexes={['(revision_id)']} />
      <Entity id={id('snapshots')} title="snapshots" detail="04_module_roots.hcl, key columns" fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'location_id', type: 'uuid', fk: true },
        { name: 'base_snapshot_id', type: 'uuid? · same root', fk: true },
        { name: 'ordinal', type: 'bigint · unique' },
        { name: 'revision', type: 'text' },
        { name: 'git_commit', type: 'text' },
        { name: 'content_set_hash', type: 'text' },
        { name: 'configuration_hash', type: 'text' },
        { name: 'context_hash', type: 'text · 64 hex' },
      ]} indexes={['(base_snapshot_id)']} />
      <Entity id={id('document')} title="document_deltas" detail="18_document_membership.hcl" accent={COLORS.fk} fields={[
        { name: 'snapshot_ordinal', type: 'bigint', pk: true, fk: true },
        { name: 'path_key', type: 'text', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'document_ordinal', type: 'bigint? · set only', fk: true },
        { name: 'operation', type: 'set | delete' },
      ]} indexes={['(document_ordinal)']} />
    </div>
    <div style={{ height: '60pt' }} />
    <div className="flex items-start justify-center" style={{ columnGap: '90px' }}>
      <Entity id={id('symbol')} title="symbol_deltas" detail="13_layered_fingerprints.hcl" accent={COLORS.fk} fields={[
        { name: 'snapshot_ordinal', type: 'bigint', pk: true, fk: true },
        { name: 'symbol_handle', type: 'bigint · symbols.handle', pk: true, fk: true },
        { name: 'root_ordinal', type: 'integer', fk: true },
        { name: 'operation', type: 'set | delete' },
        { name: 'public_fp', type: 'bigint? · set only' },
        { name: 'private_fp', type: 'bigint? · set only' },
        { name: 'content_fp', type: 'bigint? · set only' },
      ]} indexes={['(symbol_handle, snapshot_ordinal)']} />
      <Entity id={id('implements')} title="implements_deltas" detail="17_implements.hcl" accent={COLORS.fk} fields={[
        { name: 'snapshot_ordinal', type: 'bigint', pk: true, fk: true },
        { name: 'type_handle', type: 'bigint · symbols.handle', pk: true, fk: true },
        { name: 'interface_handle', type: 'bigint · symbols.handle', pk: true, fk: true },
        { name: 'root_ordinal', type: 'integer', fk: true },
        { name: 'operation', type: 'set | delete' },
      ]} indexes={['(interface_handle, snapshot_ordinal)', '(type_handle, snapshot_ordinal)']} />
      <Entity id={id('facts')} title="implements_facts" detail="17_implements.hcl: marks computed facts" accent={COLORS.outputBorder} fields={[
        { name: 'snapshot_ordinal', type: 'bigint', pk: true, fk: true },
        { name: 'root_ordinal', type: 'integer', fk: true },
        { name: 'pair_count', type: 'bigint ≥ 0' },
      ]} />
    </div>
    <div className="mx-auto mt-6 px-6" style={{ maxWidth: '1000px' }}>
      <Caption>
        Every delta table cascades with its snapshot. A <Code>set</Code> row must carry its target (<Code>revision_id</Code>, <Code>document_ordinal</Code>, or all three fingerprints) and a <Code>delete</Code> must not, by check constraint; <Code>implements_deltas</Code> has no target column. Source rows are keyed by the snapshot's uuid, the other three by its ordinal. <Code>implements_facts.pair_count</Code> is checked against a full <Code>EffectiveImplements</Code> read.
      </Caption>
    </div>
    <Arrow variant="er" from={id('source')} to={id('snapshots')} path="straight" startAnchor="right" endAnchor={{ position: 'left', offset: { y: -60 } }}
      labels={{ middle: <Label>root_id, snapshot_id</Label> }} />
    <Arrow variant="er" from={id('document')} to={id('snapshots')} path="straight" startAnchor="left" endAnchor={{ position: 'right', offset: { y: -60 } }}
      labels={{ middle: <Label>root_id, snapshot_ordinal</Label> }} />
    <Arrow variant="er" from={id('symbol')} to={id('snapshots')} path="straight" startAnchor="top" endAnchor={{ position: 'bottom', offset: { x: -70 } }}
      labels={{ middle: <Label>snapshot_ordinal</Label> }} />
    <Arrow variant="er" from={id('implements')} to={id('snapshots')} path="straight" startAnchor="top" endAnchor="bottom"
      labels={{ middle: <Label>snapshot_ordinal</Label> }} />
    <Arrow variant="er" from={id('facts')} to={id('snapshots')} path="straight" startAnchor="top" endAnchor={{ position: 'bottom', offset: { x: 70 } }}
      labels={{ middle: <Label>snapshot_ordinal</Label> }} />
  </>}</Diagram>;
}

export const DELTA_GAPS: Gap[] = [
  {
    id: 1, title: 'Checkpoint compaction without deletion', status: 'open',
    today: 'The only operation that turns a delta into a complete set of rows is rebaseSnapshot, and it runs only inside PruneHistory, when a kept snapshot loses its base to deletion.',
    endState: 'Maintenance consolidates any snapshot\'s effective bindings into a checkpoint (complete set rows, no base), preserving its id, ordinal, commit, coverage, diagnostics, dependency edges and original change stats, verified by resolved-state digests before and after.',
    source: 'docs/incremental-snapshot-plan.md §Checkpoint representation; §Phase 2',
    evidence: 'storage/prune_apply.go:49 (rebaseSnapshot, called only from prunePlan.apply)',
  },
  {
    id: 2, title: 'Physical generations on binding keys', status: 'open',
    today: 'source_deltas, document_deltas and symbol_deltas are keyed by snapshot and path or handle alone, so replacing a snapshot\'s bindings rewrites the rows readers see.',
    endState: 'Binding sets carry a generation, one active per snapshot. A checkpoint is written into an inactive generation in bounded batches and promoted by a short compare-and-swap of the snapshot\'s generation and base, after rechecking generations and the prune epoch; readers never see a staged generation. The column name and key shape are not specified.',
    source: 'docs/incremental-snapshot-plan.md §Checkpoint representation; §Phase 2',
    evidence: 'storage/migrations/05_source_deltas.hcl:74, 18_document_membership.hcl:31, 13_layered_fingerprints.hcl:39 (primary keys, no generation column)',
  },
  {
    id: 3, title: 'Bounded chain depth', status: 'open',
    today: 'No policy bounds chain depth; only explicit pruning shortens a chain (it rebases every kept snapshot whose base it deletes), and the walk fails past 100,000 links.',
    endState: 'A typed checkpoint policy bounds chain depth and accumulated changed bindings. Candidate maximum depths of 16, 32 and 64 are to be benchmarked; the doc says these are experiment values and leaves the defaults undecided.',
    source: 'docs/incremental-snapshot-plan.md §Checkpoint representation; §Phase 6',
    evidence: 'storage/effective_sources.go:15 (maxBaseChainDepth = 100_000)',
  },
  {
    id: 4, title: 'content_generation on symbol_deltas', status: 'not started',
    today: 'A delta row carries only fingerprints, so how many times a symbol\'s body changed along a chain cannot survive a rebase, which collapses history into one set per symbol.',
    endState: 'symbol_deltas.content_generation bigint NOT NULL (≥ 1, on deletes too), set by deltaRows as the base\'s value plus one when content_fp changes, and copied by rebase; storage.SymbolGenerations reads the base\'s values for the handles a publication writes. Existing databases are backfilled through a one-row symbol_generation_backfill progress table: the column is added nullable, filled in resumable batches, then tightened to NOT NULL by a follow-up migration, and OpenReadOnly refuses an incomplete backfill. Surfaces: a symbol.generation SQL field and uir history symbol. The plan names the first migration 22_symbol_generations.hcl, a number since taken by 22_producer_metrics.hcl.',
    source: 'docs/symbol-generations-tests-plan.md §1 Symbol generations',
    evidence: 'storage/migrations/13_layered_fingerprints.hcl (no such column); indexer/symbol_deltas.go:183 (deltaRows)',
  },
  {
    id: 5, title: 'symbol_handle renamed to symbol_id', status: 'proposed',
    today: 'symbol_deltas.symbol_handle references symbols.handle, beside the SHA-256 symbols.id.',
    endState: 'The column becomes symbol_id with the same integers and composite key, and its foreign key points at symbols.id, the sole bigint ID. The doc says all delta symbol keys use that one ID but names only this rename; implements_deltas.type_handle and interface_handle are left unspecified.',
    source: 'docs/symbol-reference-storage.md §Pruning and removed interfaces',
    evidence: 'storage/migrations/13_layered_fingerprints.hcl:18 and :52-55',
  },
  {
    id: 6, title: 'Affected-only extraction', status: 'open',
    today: 'The deltas a commit writes are already proportional to the change, but extraction is not: a changed-source commit still type-checks the whole module, and every package whose input hash moved is rendered again.',
    endState: 'Changed inputs are compared before loading, and only the affected packages are type-checked and lowered, in one consistent type universe per batch.',
    source: 'docs/incremental-snapshot-plan.md §Phase 3',
    evidence: 'indexer/typed_load.go:70 (loadPackages(config, "./..."))',
  },
  {
    id: 7, title: 'Durable maintenance outside publication', status: 'open',
    today: 'No table records maintenance work. Reclamation is PruneHistory, run synchronously by uir prune or by a producer inside its own publication.',
    endState: 'Publication records checkpoint and reclamation demand atomically; bounded maintenance runs later with backpressure, and reclaims only what no head, retained commit, dependency target or active lease can reach. The doc names the OIPA publisher, outside this repository, as the producer that prunes in its publication today.',
    source: 'docs/incremental-snapshot-plan.md §Commit processing step 7; §Phase 6',
    evidence: 'storage/prune_history.go:46 (PruneHistory); storage/migrations/04–22 (no maintenance table)',
  },
  {
    id: 8, title: 'Prune keeps the symbol registry', status: 'proposed',
    today: 'Prune deletes symbols that no remaining posting or delta names. A new symbol takes one past the largest handle still stored in its bucket (firstFree), so after a prune a handle value can be allocated again, to the same symbol or to a different one.',
    endState: 'Orphan-symbol deletion is removed: every committed symbols row and owner mapping, and the module, package and kind registries, survive pruning and reindexing.',
    source: 'docs/symbol-reference-storage.md §Pruning and removed interfaces; §Permanent symbol IDs',
    evidence: 'storage/prune_symbols.go:13 (deleteOrphanSymbols), called from storage/prune_apply.go:40; storage/symbol_handles.go:123 (firstFree)',
  },
  {
    id: 9, title: 'Stored per-symbol closure_fp', status: 'deferred',
    today: 'uir graph --since answers "did anything in this call graph change" by walking both snapshots at query time; no closure fingerprint is stored.',
    endState: 'An SCC-condensed closure_fp per symbol, only if the since-query misses a measured latency budget, and only after the missing call edges are closed. Deferred, so it may never be built, and therefore not drawn on the diagram.',
    source: 'docs/incremental-snapshot-plan.md §Call-graph change queries',
    evidence: 'query/graph_since.go; storage/migrations (no closure_fp column)',
  },
];

export function DeltaGapNote() {
  return <p className="text-[9pt] leading-snug" style={{ color: COLORS.muted }}>
    Not drawn: <Code>docs/symbol-generations-tests-plan.md</Code> §2–4 also plans <Code>test_cases</Code>, <Code>test_runs</Code>, <Code>test_results</Code> and <Code>test_coverage</Code>, keyed on symbol handles and <Code>documents.ordinal</Code>; prune's orphan-symbol scan would then have to count <Code>test_cases.symbol_handle</Code> as a reference. They are not delta storage, so they are left off this page.
  </p>;
}

function GapLine({ gap, children }: { gap: number; children: React.ReactNode }) {
  return <div className="flex items-start gap-1.5 text-[8.5pt] leading-snug" style={{ color: COLORS.accent }}>
    <GapMarker gap={gap} /><span>{children}</span>
  </div>;
}

export function DeltasEndState() {
  return <Diagram className="relative">{(id) => <>
    <div className="grid px-6 py-8" style={{ gridTemplateColumns: 'repeat(4, 210px)', columnGap: '64px', rowGap: '18px', justifyContent: 'center', alignItems: 'center' }}>
      <StatusBox id={id('c100')} title="C100 · checkpoint" status="built" minWidth="210px">
        <Step>Complete source, document and symbol bindings and no base, as any baseless snapshot today.</Step>
      </StatusBox>
      <StatusBox id={id('c101')} title="C101 · delta" status="built" minWidth="210px">
        <Step>Replaces two documents, deletes one source.</Step>
      </StatusBox>
      <StatusBox id={id('c102')} title="C102 · delta" status="built" minWidth="210px">
        <Step>Replaces one document.</Step>
      </StatusBox>
      <StatusBox id={id('c103')} title="C103 · delta" status="built" minWidth="210px">
        <Step>Unchanged content, new commit metadata.</Step>
      </StatusBox>
      <div style={{ gridColumn: '1 / span 4' }}>
        <GapLine gap={3}>Chain depth is bounded by a measured policy (16, 32 or 64 under benchmark); today only the 100,000-link guard stops a walk.</GapLine>
      </div>
      <div style={{ gridColumn: '1 / span 4', height: '22pt' }} />
      <div style={{ gridColumn: '1 / span 2', gridRow: 4 }}>
        <StatusBox id={id('row')} title="symbol_deltas row" status="built" minWidth="300px">
          <div className={mono}>
            <div style={{ color: COLORS.accent }}>snapshot_ordinal <span style={{ color: COLORS.muted }}>bigint, PK</span></div>
            <div className="flex items-center gap-1.5" style={{ color: COLORS.pk }}>
              <span>symbol_id <span style={{ color: COLORS.muted }}>bigint, PK → symbols.id</span></span><GapMarker gap={5} />
            </div>
            <div className="line-through" style={{ color: COLORS.muted }}>symbol_handle → symbols.handle</div>
            <div style={{ color: COLORS.accent }}>root_ordinal, operation</div>
            <div style={{ color: COLORS.accent }}>public_fp, private_fp, content_fp</div>
            <div className="flex items-center gap-1.5" style={{ color: COLORS.pk }}>
              <span>content_generation <span style={{ color: COLORS.muted }}>bigint ≥ 1</span></span><GapMarker gap={4} />
            </div>
          </div>
        </StatusBox>
      </div>
      <div style={{ gridColumn: 3, gridRow: 4 }}>
        <StatusBox id={id('maintenance')} title="Maintenance" status="planned" gap={7} minWidth="210px">
          <Step>Demand recorded with the publication; runs later in bounded batches, outside any commit's transaction.</Step>
        </StatusBox>
      </div>
      <div style={{ gridColumn: 4, gridRow: 4 }}>
        <StatusBox id={id('checkpoint')} title="C103 · checkpoint" status="planned" gap={1} minWidth="210px">
          <Step>C103's effective bindings as complete set rows; same snapshot id, ordinal, commit and stats.</Step>
          <div className="mt-1.5"><GapLine gap={2}>Written into an inactive generation, promoted by compare-and-swap.</GapLine></div>
        </StatusBox>
      </div>
      <div style={{ gridColumn: '1 / span 2', gridRow: 5 }}>
        <StatusBox id={id('orphans')} title="Orphan-symbol deletion in prune" status="retired" gap={8} minWidth="484px">
          <Step>Prune stops deleting <Code>symbols</Code> rows; every committed mapping keeps its ID.</Step>
        </StatusBox>
      </div>
      <div style={{ gridColumn: 4, gridRow: 5 }}>
        <div style={{ height: '24pt' }} />
        <StatusBox id={id('c104')} title="C104 · delta" status="built" minWidth="210px">
          <Step>Based on the C103 checkpoint, so its reads walk one link.</Step>
        </StatusBox>
      </div>
      <div style={{ gridColumn: '1 / span 4', height: '9pt' }} />
      <div style={{ gridColumn: '1 / span 2' }}>
        <StatusBox id={id('extraction')} title="Type-check affected packages only" status="planned" gap={6} minWidth="484px">
          <Step>Today every changed-source commit loads the whole module with <Code>packages.Load("./...")</Code>.</Step>
        </StatusBox>
      </div>
      <div style={{ gridColumn: '1 / span 4', height: '9pt' }} />
      <div style={{ gridColumn: '1 / span 4' }}>
        <StatusBox id={id('index-checkpoints')} title="index_checkpoints: not a snapshot checkpoint" status="built" minWidth="1032px">
          <Step>
            Migration 20 is a run memo for <Code>uir add</Code> and <Code>reindex</Code> (<Code>indexer/checkpoint.go</Code>). Keyed by a hash of the request (paths, reason, configuration, <Code>go.work</Code>), it stores the Git worktree fingerprint of each repository it saw, the modification time, size and mode of each <Code>go.mod</Code>/<Code>go.sum</Code> (and <Code>go.work</Code>/<Code>go.work.sum</Code>), a digest of the registered locations and the heads' sibling shapes, the results and the location heads. When the same request finds all of them unchanged, it returns the stored results as unchanged without extracting. It holds no bindings and is never a base.
          </Step>
        </StatusBox>
      </div>
    </div>
    <Arrow variant="secondary" from={id('c101')} to={id('c100')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="secondary" from={id('c102')} to={id('c101')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="secondary" from={id('c103')} to={id('c102')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="primary" from={id('c103')} to={id('checkpoint')} path="straight" startAnchor="bottom" endAnchor="top"
      labels={{ middle: <NodePill>consolidate</NodePill> }} />
    <Arrow variant="primary" from={id('maintenance')} to={id('checkpoint')} path="straight" startAnchor="right" endAnchor="left" />
    <Arrow variant="secondary" from={id('c104')} to={id('checkpoint')} path="straight" startAnchor="top" endAnchor="bottom" />
  </>}</Diagram>;
}

export default function DeltasPage() {
  return <Page>
    <Section title="A snapshot stores only its deltas" className="uir-deltas-chain">
      <DeltaChain />
    </Section>
    <Section title="Four delta tables around one snapshot" className="uir-deltas-four">
      <FourDeltas />
    </Section>
    <Section title="What one edit publishes" className="uir-deltas-publish">
      <PublishDeltas />
    </Section>
    <Section title="Which snapshot becomes the base" className="uir-deltas-base">
      <BaseSelection />
    </Section>
    <Section title="Pruning rebases a survivor" className="uir-deltas-prune">
      <PruneRebase />
    </Section>
    <Section title="Delta tables" className="uir-deltas-tables">
      <DeltaTables />
    </Section>
    <Section title="End state: checkpoints and short chains" className="uir-deltas-end">
      <Legend />
      <DeltasEndState />
      <GapList gaps={DELTA_GAPS} />
      <DeltaGapNote />
    </Section>
  </Page>;
}

// @live
import React from 'react';
import { Arrow, BoxNode, COLORS, Diagram, Page, Section } from '@flanksource/facet';

type Field = { name: string; type: string; pk?: boolean; fk?: boolean };

function FieldRow({ name, type, pk, fk }: Field) {
  return <div className="flex items-center gap-1.5 py-0.5">
    {pk && <span className="rounded px-1 text-[6pt] font-bold text-white" style={{ backgroundColor: COLORS.pk }}>PK</span>}
    {fk && <span className="rounded px-1 text-[6pt] font-bold text-white" style={{ backgroundColor: COLORS.fk }}>FK</span>}
    {!pk && !fk && <span className="w-[14pt]" />}
    <span className="text-[7.5pt] font-semibold" style={{ color: COLORS.accent }}>{name}</span>
    <span className="ml-auto text-[7pt]" style={{ color: COLORS.muted }}>{type}</span>
  </div>;
}

function Entity({ id, title, detail, fields, accent = COLORS.primary }: {
  id: string; title: string; detail: string; fields: Field[]; accent?: string;
}) {
  return <BoxNode id={id} title={title} headerColor={accent} bodyColor={COLORS.background} borderColor={accent} compact minWidth="250px">
    <div className="mb-2 text-[7pt]" style={{ color: COLORS.muted }}>{detail}</div>
    <div className="flex flex-col">{fields.map((field) => <FieldRow key={field.name} {...field} />)}</div>
  </BoxNode>;
}

function Label({ children }: { children: React.ReactNode }) {
  return <span className="rounded px-1 text-[6pt] font-semibold" style={{ backgroundColor: COLORS.background, color: COLORS.muted }}>{children}</span>;
}

function ModulesAndPublication() {
  return <Diagram className="relative py-8">{(id) => <>
    <div className="flex items-center justify-center gap-16 mb-16">
      <Entity id={id('module')} title="modules" detail="One logical Go module path" accent={COLORS.accent} fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_key', type: 'text · unique' },
        { name: 'name', type: 'text' },
        { name: 'created_at', type: 'timestamptz' },
      ]} />
      <Entity id={id('location')} title="locations" detail="One registered checkout or nested module" fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'canonical_path', type: 'text' },
        { name: 'parent_location_id', type: 'uuid?', fk: true },
        { name: 'mount_path', type: 'text' },
        { name: 'kind', type: 'module | git | git-submodule' },
        { name: 'repository_uri', type: 'text?' },
        { name: 'created_at', type: 'timestamptz' },
      ]} />
      <Entity id={id('snapshot')} title="snapshots" detail="Immutable delta chain per checkout; row exists only once published" accent={COLORS.fk} fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'location_id', type: 'uuid', fk: true },
        { name: 'base_snapshot_id', type: 'uuid?', fk: true },
        { name: 'revision', type: 'text' },
        { name: 'worktree_state', type: 'clean | dirty | unknown' },
        { name: 'content_set_hash', type: 'text' },
        { name: 'configuration_hash', type: 'text' },
        { name: 'context_hash', type: 'text · 64 hex' },
        { name: 'coverage', type: 'indexed | partial | syntax | excluded' },
        { name: 'package_count', type: 'integer' },
        { name: 'diagnostics', type: 'jsonb' },
        { name: 'started_at', type: 'timestamptz' },
        { name: 'completed_at', type: 'timestamptz' },
      ]} />
    </div>
    <div className="flex items-start justify-center gap-24">
      <Entity id={id('primary')} title="primary_locations" detail="Default checkout for a module" accent={COLORS.outputBorder} fields={[
        { name: 'root_id', type: 'uuid', pk: true, fk: true },
        { name: 'location_id', type: 'uuid', fk: true },
      ]} />
      <Entity id={id('head')} title="location_heads" detail="Published head per checkout, compare-and-swap version" accent={COLORS.outputBorder} fields={[
        { name: 'location_id', type: 'uuid', pk: true, fk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'snapshot_id', type: 'uuid', fk: true },
        { name: 'version', type: 'bigint' },
      ]} />
    </div>
    <Arrow variant="er" from={id('location')} to={id('module')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <Label>N:1 module</Label> }} />
    <Arrow variant="er" from={id('snapshot')} to={id('location')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <Label>N:1 checkout</Label> }} />
    <Arrow variant="er" from={id('primary')} to={id('module')} path="straight" startAnchor="top" endAnchor="bottom" />
    <Arrow variant="er" from={id('primary')} to={id('location')} path="straight" startAnchor="top" endAnchor={{ position: 'bottom', offset: { x: -50 } }} />
    <Arrow variant="er" from={id('head')} to={id('location')} path="straight" startAnchor="top" endAnchor={{ position: 'bottom', offset: { x: 50 } }} />
    <Arrow variant="er" from={id('head')} to={id('snapshot')} path="straight" startAnchor="top" endAnchor="bottom" labels={{ middle: <Label>(location_id, snapshot_id)</Label> }} />
  </>}</Diagram>;
}

function FilesAndDocuments() {
  return <Diagram className="relative py-8">{(id) => <>
    <div className="flex items-center justify-center gap-20 mb-16">
      <Entity id={id('revision')} title="source_revisions" detail="Content-addressed byte identity of one file version" accent={COLORS.accent} fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'path_key', type: 'text' },
        { name: 'content_hash', type: 'text · 64 hex' },
        { name: 'package_path', type: 'text' },
        { name: 'size_bytes', type: 'bigint' },
      ]} />
      <Entity id={id('delta')} title="source_deltas" detail="Set or delete a path in one snapshot" accent={COLORS.fk} fields={[
        { name: 'snapshot_id', type: 'uuid', pk: true, fk: true },
        { name: 'path_key', type: 'text', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'revision_id', type: 'uuid?', fk: true },
        { name: 'operation', type: 'set | delete' },
      ]} />
      <Entity id={id('snapshot-ref')} title="snapshots" detail="Snapshot reference" fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'base_snapshot_id', type: 'uuid?', fk: true },
      ]} />
    </div>
    <div className="flex items-start justify-center">
      <Entity id={id('document')} title="documents" detail="Extracted facts for one file version under one package input" accent={COLORS.primary} fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'path_key', type: 'text' },
        { name: 'source_revision_id', type: 'uuid', fk: true },
        { name: 'package_path', type: 'text' },
        { name: 'input_hash', type: 'text · 64 hex' },
        { name: 'indexer_version', type: 'text' },
        { name: 'coverage', type: 'indexed | partial | syntax | excluded' },
        { name: 'symbol_count', type: 'integer' },
        { name: 'occurrence_count', type: 'integer' },
        { name: 'content', type: 'jsonb' },
      ]} />
    </div>
    <Arrow variant="er" from={id('delta')} to={id('revision')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <Label>(root, path, id)</Label> }} />
    <Arrow variant="er" from={id('delta')} to={id('snapshot-ref')} path="straight" startAnchor="right" endAnchor="left" labels={{ middle: <Label>N:1 snapshot</Label> }} />
    <Arrow variant="er" from={id('document')} to={id('revision')} path="straight" startAnchor="top" endAnchor="bottom" labels={{ middle: <Label>(root, path, id)</Label> }} />
  </>}</Diagram>;
}

function SymbolIndex() {
  return <Diagram className="relative py-8">{(id) => <>
    <div className="flex items-center justify-center gap-20 mb-16">
      <Entity id={id('symbol')} title="symbols" detail="Canonical identity, global across modules" accent={COLORS.accent} fields={[
        { name: 'id', type: 'text · 64 hex', pk: true },
        { name: 'identity_version', type: 'integer' },
        { name: 'canonical_key', type: 'text · unique' },
        { name: 'module_key', type: 'text' },
        { name: 'package_path', type: 'text' },
        { name: 'kind', type: 'package | type | func | …' },
        { name: 'owner_id', type: 'text? · self', fk: true },
        { name: 'name', type: 'text' },
        { name: 'search_name', type: 'text' },
        { name: 'visibility', type: 'exported | internal' },
        { name: 'parameter_types', type: 'jsonb' },
      ]} />
      <Entity id={id('posting')} title="symbol_postings" detail="Inverted index: which documents mention a symbol" accent={COLORS.fk} fields={[
        { name: 'document_id', type: 'uuid', pk: true, fk: true },
        { name: 'symbol_id', type: 'text', pk: true, fk: true },
        { name: 'role', type: 'definition | reference | implements', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'occurrence_count', type: 'integer' },
      ]} />
      <Entity id={id('document-ref')} title="documents" detail="Document reference" fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'path_key', type: 'text' },
        { name: 'input_hash', type: 'text · 64 hex' },
      ]} />
    </div>
    <div className="flex items-center justify-center gap-20">
      <Entity id={id('coverage')} title="package_coverage" detail="Per-snapshot package input and export shape" accent={COLORS.outputBorder} fields={[
        { name: 'snapshot_id', type: 'uuid', pk: true, fk: true },
        { name: 'package_path', type: 'text', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'input_hash', type: 'text · 64 hex' },
        { name: 'export_shape_hash', type: 'text? · 64 hex' },
        { name: 'coverage', type: 'indexed | partial | syntax | excluded' },
        { name: 'file_count', type: 'integer' },
        { name: 'diagnostics', type: 'jsonb' },
      ]} />
      <Entity id={id('snapshot-ref2')} title="snapshots" detail="Snapshot reference" fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'context_hash', type: 'text · 64 hex' },
      ]} />
    </div>
    <Arrow variant="er" from={id('posting')} to={id('symbol')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <Label>N:1 symbol</Label> }} />
    <Arrow variant="er" from={id('posting')} to={id('document-ref')} path="straight" startAnchor="right" endAnchor="left" labels={{ middle: <Label>N:1 document</Label> }} />
    <Arrow variant="er" from={id('coverage')} to={id('snapshot-ref2')} path="straight" startAnchor="right" endAnchor="left" labels={{ middle: <Label>N:1 snapshot</Label> }} />
  </>}</Diagram>;
}

export default function UIRGormStorageDesign() {
  return <Page>
    <Section title="Modules and publication">
      <p>Module paths identify modules; canonical checkout paths identify locations. Each location has independent immutable snapshots and one published head, tied to a snapshot of that same location. A snapshot row exists only once its publication committed; it carries the context hash that triggers re-extraction when dependencies change.</p>
      <ModulesAndPublication />
    </Section>
    <Section title="Files and documents">
      <p>A source revision is the byte identity of one file version. Each snapshot stores only changed path operations; reading follows base snapshots newest-first, with delete tombstones hiding older revisions. A document holds everything extracted from a file version under one package input hash: declared symbols with shapes and hashes, occurrences with ranges, and diagnostics. Which document a snapshot activates is derived from its source revision and its package input hash, not stored.</p>
      <FilesAndDocuments />
    </Section>
    <Section title="Symbol index">
      <p>Symbols are canonical identities shared across modules. Postings are the inverted index from a symbol to the documents that define, reference, or implement it. Package coverage records, per snapshot, each package's input hash and export shape so membership can be derived and dependents invalidated.</p>
      <SymbolIndex />
      <p>PostgreSQL uses UUID, JSONB and TIMESTAMPTZ. SQLite projects the same HCL to TEXT UUIDs, validated JSON TEXT and DATETIME. See the HCL files and the schema review for full keys, checks and delete actions.</p>
    </Section>
  </Page>;
}

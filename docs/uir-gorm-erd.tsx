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

function Entity({ id, title, detail, fields, indexes = [], minWidth = '250px', accent = COLORS.primary }: {
  id: string; title: string; detail: string; fields: Field[]; indexes?: string[]; minWidth?: string; accent?: string;
}) {
  return <BoxNode id={id} title={title} headerColor={accent} bodyColor={COLORS.background} borderColor={accent} compact minWidth={minWidth}>
    <div className="mb-2 text-[7pt]" style={{ color: COLORS.muted }}>{detail}</div>
    <div className="flex flex-col">{fields.map((field) => <FieldRow key={field.name} {...field} />)}</div>
    {indexes.length > 0 && <div className="mt-3 flex flex-col gap-1 border-t pt-2 text-[7pt]" style={{ borderColor: COLORS.muted, color: COLORS.muted }}>
      {indexes.map((index) => <span key={index} className="whitespace-pre-line">INDEX {index}</span>)}
    </div>}
  </BoxNode>;
}

function Label({ children }: { children: React.ReactNode }) {
  return <span className="rounded px-1 text-[6pt] font-semibold" style={{ backgroundColor: COLORS.background, color: COLORS.muted }}>{children}</span>;
}

function SymbolRef({ id, detail }: { id: string; detail: string }) {
  return <Entity id={id} title="symbols" detail={detail} accent={COLORS.accent} fields={[
    { name: 'id', type: 'bigint · permanent · H64b', pk: true },
  ]} />;
}

function VersionRef({ id }: { id: string }) {
  return <Entity id={id} title="documents" detail="Immutable extraction version; JSON retained" fields={[
    { name: 'ordinal', type: 'bigint · unique · version_id' },
    { name: 'root_id', type: 'uuid', fk: true },
    { name: 'path_key', type: 'text' },
    { name: 'input_hash', type: 'text · 64 hex' },
  ]} />;
}

function RootRef({ id }: { id: string }) {
  return <Entity id={id} title="modules" detail="Source module" accent={COLORS.accent} fields={[
    { name: 'id', type: 'uuid', pk: true },
    { name: 'ordinal', type: 'integer · unique' },
  ]} />;
}

function ModulesAndPublication() {
  return <Diagram className="relative py-8">{(id) => <>
    <div className="flex items-center justify-center gap-16 mb-16">
      <Entity id={id('module')} title="modules" detail="One logical Go module path" accent={COLORS.accent} fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_key', type: 'text · unique' },
        { name: 'name', type: 'text' },
        { name: 'created_at', type: 'timestamptz' },
        { name: 'ordinal', type: 'integer · unique' },
      ]} />
      <Entity id={id('location')} title="locations" detail="One registered checkout or nested module" fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'canonical_path', type: 'text' },
        { name: 'parent_location_id', type: 'uuid?', fk: true },
        { name: 'mount_path', type: 'text' },
        { name: 'kind', type: 'module | git | git-submodule | external' },
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
        { name: 'ordinal', type: 'bigint · unique' },
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
    <div className="flex items-center justify-center gap-20">
      <Entity id={id('document')} title="documents" detail="Extraction version; format-4 JSON retains rich facts" accent={COLORS.primary} fields={[
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
        { name: 'content', type: 'jsonb · symbol IDs as decimal strings' },
        { name: 'ordinal', type: 'bigint · unique · version_id' },
      ]} indexes={[
        '(root_id, path_key, input_hash, id, ordinal,\nsource_revision_id, package_path, indexer_version,\ncoverage, symbol_count, occurrence_count)',
      ]} />
      <Entity id={id('status')} title="document_projection_status" detail="Proposed: written after row validation, including zero-fact versions" accent={COLORS.outputBorder} fields={[
        { name: 'version_id', type: 'bigint', pk: true, fk: true },
        { name: 'projection_version', type: 'integer · initially 1' },
        { name: 'declaration_count', type: 'integer ≥ 0' },
        { name: 'occurrence_count', type: 'integer ≥ 0 · kinds 0–5' },
        { name: 'relationship_count', type: 'integer ≥ 0 · kinds 6–7' },
      ]} />
    </div>
    <Arrow variant="er" from={id('delta')} to={id('revision')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <Label>(root, path, id)</Label> }} />
    <Arrow variant="er" from={id('delta')} to={id('snapshot-ref')} path="straight" startAnchor="right" endAnchor="left" labels={{ middle: <Label>N:1 snapshot</Label> }} />
    <Arrow variant="er" from={id('document')} to={id('revision')} path="straight" startAnchor="top" endAnchor="bottom" labels={{ middle: <Label>(root, path, id)</Label> }} />
    <Arrow variant="er" from={id('status')} to={id('document')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <Label>1:1 projection</Label> }} />
  </>}</Diagram>;
}

function SymbolLocations() {
  return <Diagram className="relative py-8">{(id) => <>
    <div className="flex items-center justify-center gap-24">
      <div className="flex flex-col items-center gap-8">
        <Entity id={id('symbol')} title="symbols" detail="Proposed: permanent registry; sole symbol ID" accent={COLORS.accent} fields={[
          { name: 'id', type: 'bigint · existing H64b handle', pk: true },
          { name: 'canonical_key', type: 'text · unique natural key' },
          { name: 'module_key', type: 'text' },
          { name: 'package_path', type: 'text' },
          { name: 'kind', type: 'text · symbol_kinds.name', fk: true },
          { name: 'owner_id', type: 'bigint? · symbols.id', fk: true },
          { name: 'name', type: 'text' },
          { name: 'search_name', type: 'text' },
          { name: 'visibility', type: 'exported | internal' },
          { name: 'parameter_types', type: 'jsonb' },
        ]} indexes={[
          '(search_name, module_key, id)',
          '(module_key, package_path, kind, owner_id, name)',
        ]} />
        <VersionRef id={id('version')} />
      </div>
      <Entity id={id('location')} title="symbol_locations" detail="Proposed: one row per saved declaration entry" minWidth="370px" accent={COLORS.fk} fields={[
        { name: 'version_id', type: 'bigint', pk: true, fk: true },
        { name: 'declaration_index', type: 'integer ≥ 0', pk: true },
        { name: 'symbol_id', type: 'bigint?', fk: true },
        { name: 'root_ordinal', type: 'integer', fk: true },
        { name: 'source_revision_id', type: 'uuid', fk: true },
        { name: 'identity_key', type: 'text' },
        { name: 'identifier', type: 'jsonb · producer ID preserved' },
        { name: 'declaration', type: 'text · shape' },
        { name: 'child_slot', type: 'text · explorer capability' },
        { name: 'name_start_line', type: 'integer · 1-based' },
        { name: 'name_start_column', type: 'integer · UTF-16' },
        { name: 'name_end_line', type: 'integer' },
        { name: 'name_end_column', type: 'integer · UTF-16' },
        { name: 'name_start_offset', type: 'bigint · 0-based' },
        { name: 'name_end_offset', type: 'bigint · exclusive' },
        { name: 'extent_start_line', type: 'integer · 1-based' },
        { name: 'extent_start_column', type: 'integer · UTF-16' },
        { name: 'extent_end_line', type: 'integer' },
        { name: 'extent_end_column', type: 'integer · UTF-16' },
        { name: 'extent_start_offset', type: 'bigint · 0-based' },
        { name: 'extent_end_offset', type: 'bigint · exclusive' },
      ]} indexes={[
        '(symbol_id, root_ordinal, version_id, declaration_index)',
        '(source_revision_id, name_start_offset, version_id)',
      ]} />
      <div className="flex flex-col items-center gap-8">
        <Entity id={id('source')} title="source_revisions" detail="File identity and path; existing table" accent={COLORS.accent} fields={[
          { name: 'id', type: 'uuid', pk: true },
          { name: 'root_id', type: 'uuid', fk: true },
          { name: 'path_key', type: 'text' },
          { name: 'content_hash', type: 'text · 64 hex' },
          { name: 'package_path', type: 'text' },
          { name: 'size_bytes', type: 'bigint' },
        ]} />
        <RootRef id={id('root')} />
      </div>
    </div>
    <Arrow variant="er" from={id('location')} to={id('symbol')} path="straight" startAnchor={{ position: 'left', offset: { y: -120 } }} endAnchor="right" labels={{ middle: <Label>N:0..1 symbol</Label> }} />
    <Arrow variant="er" from={id('location')} to={id('version')} path="straight" startAnchor={{ position: 'left', offset: { y: 180 } }} endAnchor="right" labels={{ middle: <Label>N:1 version</Label> }} />
    <Arrow variant="er" from={id('location')} to={id('source')} path="straight" startAnchor={{ position: 'right', offset: { y: -100 } }} endAnchor="left" labels={{ middle: <Label>N:1 file revision</Label> }} />
    <Arrow variant="er" from={id('location')} to={id('root')} path="straight" startAnchor={{ position: 'right', offset: { y: 140 } }} endAnchor="left" labels={{ middle: <Label>N:1 module</Label> }} />
  </>}</Diagram>;
}

function SymbolReferences() {
  return <Diagram className="relative py-8">{(id) => <>
    <div className="flex items-center justify-center gap-24">
      <Entity id={id('reference')} title="symbol_references" detail="Proposed: each occurrence and declared relationship retains its site" minWidth="410px" accent={COLORS.fk} fields={[
        { name: 'version_id', type: 'bigint', pk: true, fk: true },
        { name: 'reference_index', type: 'integer ≥ 0', pk: true },
        { name: 'root_ordinal', type: 'integer', fk: true },
        { name: 'from_symbol_id', type: 'bigint?', fk: true },
        { name: 'from_key', type: 'text · enclosing identity' },
        { name: 'to_symbol_id', type: 'bigint?', fk: true },
        { name: 'kind', type: 'integer · 0–7' },
        { name: 'start_line', type: 'integer · 1-based' },
        { name: 'start_column', type: 'integer · UTF-16' },
        { name: 'end_line', type: 'integer' },
        { name: 'end_column', type: 'integer · UTF-16' },
        { name: 'start_offset', type: 'bigint · 0-based' },
        { name: 'end_offset', type: 'bigint · exclusive' },
        { name: 'target', type: 'jsonb? · call locator' },
        { name: 'statement_path', type: 'text' },
        { name: 'text', type: 'text' },
        { name: 'note', type: 'text · unresolved explanation' },
        { name: 'resolvable', type: 'boolean' },
        { name: 'local_root', type: 'boolean' },
      ]} indexes={[
        '(from_symbol_id, kind, root_ordinal, version_id, reference_index)',
        '(to_symbol_id, kind, root_ordinal, version_id, reference_index)',
      ]} />
      <div className="flex flex-col items-center gap-6">
        <SymbolRef id={id('from')} detail="Source/enclosing symbol; nullable at file scope" />
        <SymbolRef id={id('to')} detail="Canonical target; nullable when unresolved" />
        <VersionRef id={id('version')} />
        <RootRef id={id('root')} />
      </div>
    </div>
    <Arrow variant="er" from={id('reference')} to={id('from')} path="straight" startAnchor={{ position: 'right', offset: { y: -180 } }} endAnchor="left" labels={{ middle: <Label>N:0..1 from</Label> }} />
    <Arrow variant="er" from={id('reference')} to={id('to')} path="straight" startAnchor={{ position: 'right', offset: { y: -60 } }} endAnchor="left" labels={{ middle: <Label>N:0..1 to</Label> }} />
    <Arrow variant="er" from={id('reference')} to={id('version')} path="straight" startAnchor={{ position: 'right', offset: { y: 60 } }} endAnchor="left" labels={{ middle: <Label>N:1 source version</Label> }} />
    <Arrow variant="er" from={id('reference')} to={id('root')} path="straight" startAnchor={{ position: 'right', offset: { y: 180 } }} endAnchor="left" labels={{ middle: <Label>N:1 module</Label> }} />
  </>}</Diagram>;
}

function SnapshotMembership() {
  return <Diagram className="relative py-8">{(id) => <>
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
      <Entity id={id('snapshot')} title="snapshots" detail="Selected snapshot; source deltas resolve active paths" fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'context_hash', type: 'text · 64 hex' },
        { name: 'base_snapshot_id', type: 'uuid?', fk: true },
      ]} />
      <VersionRef id={id('version')} />
    </div>
    <Arrow variant="er" from={id('coverage')} to={id('snapshot')} path="straight" startAnchor="right" endAnchor="left" labels={{ middle: <Label>N:1 snapshot</Label> }} />
  </>}</Diagram>;
}

function SymbolAllocation() {
  return <Diagram className="relative py-8">{(id) => <>
    <div className="flex items-center justify-center gap-20 mb-12">
      <Entity id={id('symbol-module')} title="symbol_modules" detail="Numbers the 11-bit module field; '' is 0, std is 1" accent={COLORS.accent} fields={[
        { name: 'number', type: 'integer · 0–2047', pk: true },
        { name: 'module_key', type: 'text · unique' },
      ]} />
      <Entity id={id('symbol-package')} title="symbol_packages" detail="Numbers each module's packages for the 16-bit package field" accent={COLORS.accent} fields={[
        { name: 'module_number', type: 'integer', pk: true, fk: true },
        { name: 'number', type: 'integer · 0–65535', pk: true },
        { name: 'package_path', type: 'text · unique per module' },
      ]} />
    </div>
    <div className="flex items-center justify-center gap-20">
      <Entity id={id('layout')} title="symbol_layout" detail="Retained layout; incompatible changes require an upgrade" accent={COLORS.outputBorder} fields={[
        { name: 'id', type: 'integer · always 1', pk: true },
        { name: 'layout', type: 'text · H64b' },
      ]} />
      <Entity id={id('kind')} title="symbol_kinds" detail="Permanent kind codes; zero is reserved" accent={COLORS.accent} fields={[
        { name: 'code', type: 'smallint · 1–63', pk: true },
        { name: 'name', type: 'text · unique' },
        { name: 'category', type: 'container | type | callable | member' },
        { name: 'namespace', type: 'text' },
      ]} />
      <Entity id={id('symbol')} title="symbols" detail="Permanent mappings, including inactive symbols" fields={[
        { name: 'id', type: 'bigint · H64b', pk: true },
        { name: 'canonical_key', type: 'text · unique natural key' },
        { name: 'kind', type: 'text · symbol_kinds.name', fk: true },
      ]} />
    </div>
    <Arrow variant="er" from={id('symbol-package')} to={id('symbol-module')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <Label>N:1 module number</Label> }} />
    <Arrow variant="er" from={id('symbol')} to={id('kind')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <Label>N:1 kind name</Label> }} />
  </>}</Diagram>;
}

function SymbolDeltas() {
  return <Diagram className="relative py-8">{(id) => <>
    <div className="flex items-center justify-center mb-16">
      <SymbolRef id={id('symbol-ref')} detail="Sole bigint ID; mappings survive all version pruning" />
    </div>
    <div className="flex items-start justify-center gap-20">
      <Entity id={id('module-ref')} title="modules" detail="Module reference" fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'ordinal', type: 'integer · unique' },
      ]} />
      <Entity id={id('symbol-delta')} title="symbol_deltas" detail="How a snapshot's defined-symbol set differs from its base's" accent={COLORS.fk} fields={[
        { name: 'snapshot_ordinal', type: 'bigint', pk: true, fk: true },
        { name: 'symbol_id', type: 'bigint', pk: true, fk: true },
        { name: 'root_ordinal', type: 'integer', fk: true },
        { name: 'operation', type: 'set | delete' },
        { name: 'shape_fp', type: 'bigint? · set only' },
        { name: 'body_fp', type: 'bigint? · set only' },
      ]} indexes={['(symbol_id, snapshot_ordinal)']} />
      <Entity id={id('snapshot-ref3')} title="snapshots" detail="Snapshot reference" fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'base_snapshot_id', type: 'uuid?', fk: true },
        { name: 'ordinal', type: 'bigint · unique' },
      ]} />
    </div>
    <Arrow variant="er" from={id('symbol-delta')} to={id('symbol-ref')} path="straight" startAnchor="top" endAnchor="bottom" labels={{ middle: <Label>N:1 symbol ID</Label> }} />
    <Arrow variant="er" from={id('symbol-delta')} to={id('module-ref')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <Label>N:1 root</Label> }} />
    <Arrow variant="er" from={id('symbol-delta')} to={id('snapshot-ref3')} path="straight" startAnchor="right" endAnchor="left" labels={{ middle: <Label>N:1 snapshot</Label> }} />
  </>}</Diagram>;
}

export default function UIRGormStorageDesign() {
  return <Page>
    <Section title="Modules and publication">
      <p><strong>Proposed end state, revised 2026-10-08.</strong> The existing 64-bit handle becomes symbols.id, the sole symbol ID. Aggregate symbol_postings is replaced by symbol_locations and symbol_references; rich document facts remain. The <a href="symbol-reference-storage.md">detailed plan</a> specifies permanent mappings, producer allocation, ID migration and reader cutover. The current HCL still implements hashed IDs and postings; this is a design update.</p>
      <p>Module paths identify modules; canonical checkout paths identify locations. Each location has independent immutable snapshots and one published head, tied to a snapshot of that same location. A snapshot row exists only once its publication committed; it carries the context hash that triggers re-extraction when dependencies change.</p>
      <ModulesAndPublication />
    </Section>
    <Section title="Files and extraction versions">
      <p>A source revision identifies file bytes. A document identifies an extraction under one package input hash; its ordinal is the new tables' version_id. Snapshots derive active versions through source changes and package coverage. Format-4 JSON encodes symbol IDs as decimal strings, preserving 64-bit precision. Migration rewrites only symbol-ID fields and the format; source bytes, rich facts, extraction keys and recorded provenance survive. Every version has a complete projection status, even when empty.</p>
      <FilesAndDocuments />
    </Section>
    <Section title="Symbol declaration locations">
      <p>Each saved declaration entry has a location, including unproven entries with no symbol ID. All symbol FKs reference symbols.id, including owner_id. The unique canonical_key recognizes complete metadata and is an internal natural key. File paths and hashes come from source_revisions. Lines and UTF-16 columns are one-based; range ends are exclusive. Byte spans are zero-based and half-open. Root and source ownership must match the version.</p>
      <SymbolLocations />
    </Section>
    <Section title="Symbol references">
      <p>Each occurrence retains its source extraction version, enclosing symbol ID, target symbol ID, kind and exact site. Several calls between the same symbols remain separate rows. File scope has no enclosing ID; unresolved targets keep their locator and note. Selected snapshots supply target declaration versions. Locations and references answer graph/position queries without full-document decoding. API symbol IDs use decimal strings; generic graph node keys remain strings for symbols, file scope and unresolved nodes.</p>
      <SymbolReferences />
      <p>Kind codes: 0 definition, 1 reference, 2 call, 3 write, 4 type, 5 import, 6 implements, 7 embeds. Implements/embeds require both IDs and use the declaring symbol's name range; dispatch is derived from selected interface relationships. Version FKs cascade on deletion; symbol FKs restrict it. The primary key preserves occurrence order and duplicates.</p>
    </Section>
    <Section title="Snapshot membership">
      <p>Effective source revisions plus package input hashes select versions by (root_id, path_key, input_hash). Exact paired lookups use a covering metadata index. The document box below is a derived selection, not a stored snapshot-to-document FK. Shared versions reuse reference and location rows across snapshots and checkouts.</p>
      <SnapshotMembership />
    </Section>
    <Section title="Permanent symbol ID allocation">
      <p>IDs keep the H64b layout: 0 | module 11 | package 16 | visibility 1 | kind 6 | local 29. Storage resolves owners and complete natural keys, then allocates only missing symbols. Producers submit metadata and publication-local references. Every committed mapping and allocation registry survives pruning and reindexing; conflicting packed metadata is an error. There is no portable identity, SHA symbol ID, duplicate handle column or retained hash alias.</p>
      <SymbolAllocation />
    </Section>
    <Section title="Snapshot symbol deltas">
      <p>Symbol deltas record which symbols a snapshot defines, redefines or stops defining. Their symbol_id values remain the existing handles; the base chain folds set/delete operations into the selected symbol set. Registry presence alone does not make a symbol active. Pruning removes dead versions and projections while retaining every symbol ID and its owner mappings.</p>
      <SymbolDeltas />
      <p>PostgreSQL uses UUID, JSONB and TIMESTAMPTZ. SQLite projects portable HCL to text UUIDs, validated JSON text and datetime. The current HCL describes the implemented schema; the linked proposed design defines the new keys, constraints, indexes, API counter changes and validation gates.</p>
    </Section>
  </Page>;
}

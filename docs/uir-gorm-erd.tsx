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

function RootAndPublication() {
  return <Diagram className="relative py-8">{(id) => <>
    <div className="flex items-start justify-center gap-16 mb-16">
      <Entity id={id('root')} title="uir_module_roots" detail="One logical Go module path" accent={COLORS.accent} fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_key', type: 'text · unique' },
        { name: 'name', type: 'text' },
        { name: 'created_at', type: 'timestamptz' },
      ]} />
      <Entity id={id('location')} title="uir_module_locations" detail="One registered checkout or nested module" fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'canonical_path', type: 'text' },
        { name: 'parent_location_id', type: 'uuid?', fk: true },
        { name: 'mount_path', type: 'text' },
        { name: 'kind', type: 'text' },
        { name: 'repository_uri', type: 'text?' },
        { name: 'created_at', type: 'timestamptz' },
      ]} />
      <Entity id={id('snapshot')} title="uir_module_snapshots" detail="Immutable delta chain per checkout" accent={COLORS.fk} fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'location_id', type: 'uuid', fk: true },
        { name: 'base_snapshot_id', type: 'uuid?', fk: true },
        { name: 'state', type: 'text' },
        { name: 'revision', type: 'text' },
        { name: 'content_set_hash', type: 'text' },
        { name: 'configuration_hash', type: 'text' },
        { name: 'extractor_version', type: 'text' },
        { name: 'started_at', type: 'timestamptz' },
        { name: 'completed_at', type: 'timestamptz?' },
      ]} />
    </div>
    <div className="flex items-start justify-center gap-24">
      <Entity id={id('primary')} title="uir_module_primaries" detail="Preferred checkout for a root" accent={COLORS.outputBorder} fields={[
        { name: 'root_id', type: 'uuid', pk: true, fk: true },
        { name: 'location_id', type: 'uuid', fk: true },
      ]} />
      <Entity id={id('head')} title="uir_module_location_heads" detail="Atomic published head per checkout" accent={COLORS.outputBorder} fields={[
        { name: 'location_id', type: 'uuid', pk: true, fk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'snapshot_id', type: 'uuid', fk: true },
        { name: 'version', type: 'bigint' },
      ]} />
    </div>
    <Arrow variant="er" from={id('location')} to={id('root')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <Label>N:1 root</Label> }} />
    <Arrow variant="er" from={id('snapshot')} to={id('location')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <Label>N:1 checkout</Label> }} />
    <Arrow variant="er" from={id('primary')} to={id('root')} path="straight" startAnchor="top" endAnchor="bottom" />
    <Arrow variant="er" from={id('primary')} to={id('location')} path="straight" startAnchor="top" endAnchor="bottom" />
    <Arrow variant="er" from={id('head')} to={id('location')} path="straight" startAnchor="top" endAnchor="bottom" />
    <Arrow variant="er" from={id('head')} to={id('snapshot')} path="straight" startAnchor="top" endAnchor="bottom" />
  </>}</Diagram>;
}

function SourceDeltas() {
  return <Diagram className="relative py-8">{(id) => <>
    <div className="flex items-start justify-center gap-20">
      <Entity id={id('revision')} title="uir_source_revisions" detail="Content-addressed AST projection, reusable across snapshots" accent={COLORS.accent} fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'path_key', type: 'text' },
        { name: 'content_hash', type: 'text' },
        { name: 'package_path', type: 'text' },
        { name: 'extractor_version', type: 'text' },
        { name: 'size_bytes', type: 'bigint' },
        { name: 'projection', type: 'jsonb' },
      ]} />
      <Entity id={id('delta')} title="uir_source_deltas" detail="Set or delete a path in one snapshot" accent={COLORS.fk} fields={[
        { name: 'snapshot_id', type: 'uuid', pk: true, fk: true },
        { name: 'path_key', type: 'text', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'revision_id', type: 'uuid?', fk: true },
        { name: 'operation', type: 'set | delete' },
      ]} />
      <Entity id={id('snapshot-ref')} title="uir_module_snapshots" detail="Snapshot reference" fields={[
        { name: 'id', type: 'uuid', pk: true },
        { name: 'root_id', type: 'uuid', fk: true },
        { name: 'base_snapshot_id', type: 'uuid?', fk: true },
      ]} />
    </div>
    <Arrow variant="er" from={id('delta')} to={id('revision')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <Label>set → revision</Label> }} />
    <Arrow variant="er" from={id('delta')} to={id('snapshot-ref')} path="straight" startAnchor="right" endAnchor="left" labels={{ middle: <Label>N:1 snapshot</Label> }} />
  </>}</Diagram>;
}

export default function UIRGormStorageDesign() {
  return <Page>
    <Section title="Module roots and publication">
      <p>Module paths identify roots; canonical checkout paths identify locations. Each location has independent immutable snapshots and one published head.</p>
      <RootAndPublication />
    </Section>
    <Section title="Effective source view">
      <p>A source revision stores the Go AST projection. Each snapshot stores only changed path operations; reading follows base snapshots newest-first, with delete tombstones hiding older revisions.</p>
      <SourceDeltas />
      <p>PostgreSQL uses UUID, JSONB and TIMESTAMPTZ. SQLite projects the same HCL to TEXT UUIDs, validated JSON TEXT and DATETIME. See the HCL files for full keys, checks and delete actions.</p>
    </Section>
  </Page>;
}

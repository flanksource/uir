// @live
import React from 'react';
import Arrow from '@flanksource/facet/diagram/Arrow';
import { COLORS } from '@flanksource/facet/diagram/colors';
import Diagram from '@flanksource/facet/diagram/Diagram';
import Page from '@flanksource/facet/Page';
import Section from '@flanksource/facet/Section';
import { GapList, GapMarker, Legend, StatusBox, type Gap } from './uir-diagram-gaps';
import { Entity, Label } from './uir-gorm-erd';
import { Block, Caption, mono } from './uir-how-it-works';

function Code({ children }: { children: React.ReactNode }) {
  return <code className={mono} style={{ color: COLORS.accent }}>{children}</code>;
}

function Text({ children }: { children: React.ReactNode }) {
  return <p className="text-[9pt] leading-snug" style={{ color: COLORS.muted }}>{children}</p>;
}

function BandLabel({ title, children }: { title: string; children: React.ReactNode }) {
  return <div className="flex h-full flex-col justify-center border-l-4 pl-2" style={{ borderColor: COLORS.primary }}>
    <span className="text-[11pt] font-bold" style={{ color: COLORS.accent }}>{title}</span>
    <span className="text-[8.5pt] leading-snug" style={{ color: COLORS.muted }}>{children}</span>
  </div>;
}

export function StorageLayers() {
  return <Diagram className="relative">{(id) => <>
    <div className="grid px-6 py-8" style={{
      gridTemplateColumns: '104px repeat(4, 185px)', columnGap: '60px', rowGap: '64px', alignItems: 'center', justifyItems: 'start',
    }}>
      <div style={{ gridColumn: 1, gridRow: 1, alignSelf: 'stretch' }}>
        <BandLabel title="Where">Modules and their checkouts</BandLabel>
      </div>
      <div style={{ gridColumn: 2, gridRow: 1 }}>
        <Block id={id('modules')} title="modules" width="185px">
          <Text>One module, keyed by <Code>root_key</Code>. <Code>ordinal</Code> is the compact root number that postings and the symbol and implements deltas store.</Text>
        </Block>
      </div>
      <div style={{ gridColumn: 3, gridRow: 1 }}>
        <Block id={id('locations')} title="locations" width="185px">
          <Text>One checkout on disk, unique by <Code>(root_id, canonical_path)</Code>; <Code>kind</Code> is module, git, git-submodule or external.</Text>
        </Block>
      </div>
      <div style={{ gridColumn: 4, gridRow: 1 }}>
        <Block id={id('heads')} title="location_heads" width="185px">
          <Text>The published snapshot of each location. Publication moves it only if <Code>version</Code> still matches: compare-and-swap.</Text>
        </Block>
      </div>

      <div style={{ gridColumn: 1, gridRow: 2, alignSelf: 'stretch' }}>
        <BandLabel title="When">Immutable snapshots, stored as deltas</BandLabel>
      </div>
      <div style={{ gridColumn: '2 / span 2', gridRow: 2 }}>
        <Block id={id('deltas')} title="Snapshot deltas" width="430px">
          <div className="mb-1 grid grid-cols-2 gap-x-2">
            <Code>source_deltas</Code><Code>document_deltas</Code>
            <Code>symbol_deltas</Code><Code>implements_deltas</Code>
          </div>
          <Text>Each snapshot stores only <Code>set</Code> and <Code>delete</Code> rows against its base; readers take the newest row per key along the chain. The Deltas page draws them.</Text>
        </Block>
      </div>
      <div style={{ gridColumn: 4, gridRow: 2 }}>
        <Block id={id('snapshots')} title="snapshots" width="185px">
          <Text>One indexing run of a location. <Code>base_snapshot_id</Code> names the snapshot its deltas apply to.</Text>
        </Block>
      </div>

      <div style={{ gridColumn: 1, gridRow: 3, alignSelf: 'stretch' }}>
        <BandLabel title="What">Immutable facts, shared by every snapshot that has them</BandLabel>
      </div>
      <div style={{ gridColumn: 2, gridRow: 3 }}>
        <Block id={id('revisions')} title="source_revisions" width="185px">
          <Text>One file version: <Code>path_key</Code>, <Code>content_hash</Code>, <Code>package_path</Code>. No bytes; <Code>source_blobs</Code> keeps them for files without a Git path in dirty, non-Git or analysis-mode snapshots.</Text>
        </Block>
      </div>
      <div style={{ gridColumn: 3, gridRow: 3 }}>
        <Block id={id('documents')} title="documents" width="185px">
          <Text>The facts of one rendering of a file version as JSON, unique by <Code>(root_id, path_key, content_digest)</Code>.</Text>
        </Block>
      </div>
      <div style={{ gridColumn: 4, gridRow: 3 }}>
        <Block id={id('postings')} title="symbol_postings" width="185px">
          <Text>Which documents mention a symbol, and how often, per role. No positions.</Text>
        </Block>
      </div>
      <div style={{ gridColumn: 5, gridRow: 3 }}>
        <Block id={id('symbols')} title="symbols" width="185px">
          <Text>One row per canonical symbol: SHA-256 <Code>id</Code>, H64b <Code>handle</Code> and its metadata. <Code>symbol_deltas</Code> name it by handle.</Text>
        </Block>
      </div>
    </div>
    <Arrow variant="er" from={id('locations')} to={id('modules')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="er" from={id('heads')} to={id('locations')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="er" from={id('heads')} to={id('snapshots')} path="straight" startAnchor="bottom" endAnchor="top"
      labels={{ middle: <Label>snapshot_id</Label> }} />
    <Arrow variant="er" from={id('deltas')} to={id('snapshots')} path="straight" startAnchor="right" endAnchor="left" />
    <Arrow variant="er" from={id('deltas')} to={id('revisions')} path="straight"
      startAnchor={{ position: 'bottom', offset: { x: -122 } }} endAnchor="top" labels={{ middle: <Label>revision_id</Label> }} />
    <Arrow variant="er" from={id('deltas')} to={id('documents')} path="straight"
      startAnchor={{ position: 'bottom', offset: { x: 122 } }} endAnchor="top" labels={{ middle: <Label>document_ordinal</Label> }} />
    <Arrow variant="er" from={id('documents')} to={id('revisions')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="er" from={id('postings')} to={id('documents')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="er" from={id('postings')} to={id('symbols')} path="straight" startAnchor="right" endAnchor="left" />
  </>}</Diagram>;
}

function IdentityField({ name, value }: { name: string; value: string }) {
  return <div className="grid gap-x-1.5 py-0.5" style={{ gridTemplateColumns: '100px 1fr' }}>
    <span className={mono} style={{ color: COLORS.muted }}>{name}</span>
    <span className={mono} style={{ color: COLORS.accent }}>{value}</span>
  </div>;
}

const CANONICAL_KEY_FIELDS = [
  '10:uir-symbol,', '1:2,', '16:example.org/acme,', '22:example.org/acme/store,', '6:method,', '2:ab,', '4:Save,',
  '1:2,', '15:context.Context,', '30:example.org/acme/store.Invoice,', '1:0,', '0:,',
];

function BitField({ name, bits, width, detail }: { name: string; bits: string; width: string; detail: string }) {
  return <div className="flex shrink-0 flex-col items-center justify-center border-r bg-white py-1 last:border-r-0"
    style={{ width, borderColor: COLORS.primary }}>
    <span className={`font-bold ${mono}`} style={{ color: COLORS.accent }}>{name}</span>
    <span className="text-[8pt]" style={{ color: COLORS.muted }}>{bits}</span>
    <span className="text-[8pt]" style={{ color: COLORS.muted }}>{detail}</span>
  </div>;
}

export function SymbolIdentity() {
  return <Diagram className="relative">{(id) => <><div className="px-6 py-8">
    <div className="flex items-center" style={{ gap: '64px' }}>
      <Block id={id('identity')} title="indexer.Identity" width="334px">
        <IdentityField name="module_key" value="example.org/acme" />
        <IdentityField name="package_path" value="example.org/acme/store" />
        <IdentityField name="kind" value="method" />
        <IdentityField name="owner_id" value="ab" />
        <IdentityField name="name" value="Save" />
        <IdentityField name="parameters" value="context.Context," />
        <IdentityField name="" value="example.org/acme/store.Invoice" />
        <IdentityField name="results" value="none" />
        <IdentityField name="discriminator" value="empty" />
        <Caption>The example of indexer/symbol_identity_test.go; owner_id is the owner's id.</Caption>
      </Block>
      <Block id={id('key')} title="symbols.canonical_key" command="Identity.CanonicalKey" width="300px">
        <div className="rounded-md border bg-white px-2 py-1.5" style={{ borderColor: COLORS.primary }}>
          {CANONICAL_KEY_FIELDS.map((field, index) => <div key={index} className={mono} style={{ color: COLORS.accent }}>{field}</div>)}
        </div>
        <Caption>Domain, version 2, then every field as length:bytes, lists prefixed by their count.</Caption>
      </Block>
      <Block id={id('id')} title="symbols.id" command="SymbolID" width="260px">
        <div className="rounded-md border bg-white px-2 py-1.5" style={{ borderColor: COLORS.primary }}>
          <div className={mono} style={{ color: COLORS.accent }}>174c8cd1f04d6bf918d44d297124a312</div>
          <div className={mono} style={{ color: COLORS.accent }}>70c187e655634fb982c40517ea7e69a3</div>
        </div>
        <Caption>Hex SHA-256 of the key: the id documents, the API and owner_id carry.</Caption>
      </Block>
    </div>
    <div style={{ height: '72px' }} />
    <div className="flex">
      <Block id={id('handle')} title="symbols.handle · H64b bigint" command="AssignSymbolHandles" width="1008px">
        <div className="flex overflow-hidden rounded-md border bg-white" style={{ borderColor: COLORS.primary }}>
          <BitField name="0" bits="1 bit" width="30px" detail="sign" />
          <BitField name="module" bits="11 bits" width="175px" detail="symbol_modules.number" />
          <BitField name="package" bits="16 bits" width="195px" detail="symbol_packages.number" />
          <BitField name="visibility" bits="1 bit" width="80px" detail="1 exported" />
          <BitField name="kind" bits="6 bits" width="170px" detail="symbol_kinds.code" />
          <BitField name="local" bits="29 bits" width="334px" detail="dense per bucket" />
        </div>
        <Caption>Field widths are not to scale. Postings and deltas store this number instead of the 64-hex id; handles sort by module, package, visibility and kind, so each prefix is one range scan.</Caption>
      </Block>
    </div>
    <div style={{ height: '64px' }} />
    <div className="flex items-start" style={{ paddingLeft: '12px' }}>
      <div style={{ width: '30px' }} />
      <div className="flex justify-center" style={{ width: '175px' }}>
        <Block id={id('modules')} title="symbol_modules" width="165px">
          <Text>module_key → number: 0 builtins, 1 std, then from 2.</Text>
        </Block>
      </div>
      <div className="flex justify-center" style={{ width: '195px' }}>
        <Block id={id('packages')} title="symbol_packages" width="180px">
          <Text>(module_number, package_path) → number.</Text>
        </Block>
      </div>
      <div style={{ width: '80px' }} />
      <div className="flex justify-center" style={{ width: '170px' }}>
        <Block id={id('kinds')} title="symbol_kinds" width="160px">
          <Text>name → code 1–63; builtins from 1, custom kinds from 63 down.</Text>
        </Block>
      </div>
      <div className="flex justify-center" style={{ width: '334px' }}>
        <Block id={id('local')} title="next local" width="230px">
          <Text>One past the largest stored handle of the (module, package, visibility, kind) bucket.</Text>
        </Block>
      </div>
    </div>
    </div>
    <Arrow variant="primary" from={id('identity')} to={id('key')} path="straight" startAnchor="right" endAnchor="left" />
    <Arrow variant="primary" from={id('key')} to={id('id')} path="straight" startAnchor="right" endAnchor="left" />
    <Arrow variant="secondary" from={id('identity')} to={id('handle')} path="straight" startAnchor="bottom"
      endAnchor={{ position: 'top', offset: { x: -337 } }} labels={{ middle: <Label>module, package, visibility, kind</Label> }} />
    <Arrow variant="er" from={id('modules')} to={id('handle')} path="straight" startAnchor="top"
      endAnchor={{ position: 'bottom', offset: { x: -374 } }} />
    <Arrow variant="er" from={id('packages')} to={id('handle')} path="straight" startAnchor="top"
      endAnchor={{ position: 'bottom', offset: { x: -189 } }} />
    <Arrow variant="er" from={id('kinds')} to={id('handle')} path="straight" startAnchor="top"
      endAnchor={{ position: 'bottom', offset: { x: 73 } }} />
    <Arrow variant="er" from={id('local')} to={id('handle')} path="straight" startAnchor="top"
      endAnchor={{ position: 'bottom', offset: { x: 325 } }} />
  </>}</Diagram>;
}

export function FileToFacts() {
  return <Diagram className="relative">{(id) => <>
    <div className="grid px-6 py-8" style={{
      gridTemplateColumns: 'repeat(4, 220px)', columnGap: '64px', rowGap: '64px', alignItems: 'center', justifyItems: 'start',
    }}>
      <div style={{ gridColumn: 1, gridRow: 1 }}>
        <Block id={id('bytes')} title="store/store.go" width="220px">
          <Text>The file's bytes in the checkout, or at the snapshot's Git commit.</Text>
        </Block>
      </div>
      <div style={{ gridColumn: 2, gridRow: 1 }}>
        <Block id={id('revision')} title="source_revisions" width="220px">
          <Text><Code>content_hash</Code> is the SHA-256 of the bytes; the row keeps path, package and size, never the bytes.</Text>
        </Block>
      </div>
      <div style={{ gridColumn: 3, gridRow: 1 }}>
        <Block id={id('extract')} title="Extraction" width="220px">
          <div className="flex flex-col gap-1">
            <Text><span className="font-bold">typed</span>: go/types; coverage <Code>indexed</Code> or <Code>partial</Code></Text>
            <Text><span className="font-bold">syntax</span>: <Code>syntax.Extract</Code>, go/ast only; <Code>syntax</Code></Text>
            <Text><span className="font-bold">excluded</span>: no facts; <Code>excluded</Code></Text>
          </div>
        </Block>
      </div>
      <div style={{ gridColumn: 4, gridRow: 1 }}>
        <Block id={id('json')} title="DocumentContent" width="220px">
          <Text>Format 11 JSON: <Code>symbols</Code> with 64-hex ids and name and extent ranges, <Code>occurrences</Code> with symbol, role, range and enclosing, diagnostics, imports.</Text>
        </Block>
      </div>

      <div style={{ gridColumn: 1, gridRow: 2 }}>
        <Block id={id('unchanged')} title="Base package unchanged" width="220px">
          <Text>The package's input hash equals its base <Code>package_coverage</Code> row: every file keeps the base's bound document and ordinal, with no extraction (<Code>reuseBaseDocuments</Code>).</Text>
        </Block>
      </div>
      <div style={{ gridColumn: 2, gridRow: 2 }}>
        <Block id={id('postings')} title="symbol_postings" width="220px">
          <Text>Per (symbol handle, role): <Code>occurrence_count</Code> in this document, under its <Code>document_ordinal</Code>.</Text>
          <Text>Also written: <Code>declaration_metrics</Code>, one row per declaration with its name range.</Text>
        </Block>
      </div>
      <div style={{ gridColumn: 3, gridRow: 2 }}>
        <Block id={id('document')} title="documents" command="CreateDocument" width="220px">
          <Text>New row under the next <Code>ordinal</Code>, keyed by <Code>content_digest</Code>, with <Code>input_hash</Code> kept as provenance.</Text>
        </Block>
      </div>
      <div style={{ gridColumn: 4, gridRow: 2 }}>
        <Block id={id('digest')} title="DocumentDigest" width="220px">
          <Text>SHA-256 over source revision id, package path, indexer version, coverage and canonical content. Not the package input hash.</Text>
        </Block>
      </div>

      <div style={{ gridColumn: 3, gridRow: 3 }}>
        <Block id={id('binding')} title="document_deltas" width="220px">
          <Text>A <Code>set</Code> row binds the path to that document ordinal in this snapshot. The Deltas page draws it.</Text>
        </Block>
      </div>
      <div style={{ gridColumn: 4, gridRow: 3 }}>
        <Block id={id('reuse')} title="Digest already stored" width="220px">
          <Text><Code>existingDocuments</Code> finds it: the stored document is reused with its ordinal and its postings; nothing is inserted.</Text>
        </Block>
      </div>
    </div>
    <Arrow variant="primary" from={id('bytes')} to={id('revision')} path="straight" startAnchor="right" endAnchor="left" />
    <Arrow variant="primary" from={id('revision')} to={id('extract')} path="straight" startAnchor="right" endAnchor="left" />
    <Arrow variant="secondary" from={id('revision')} to={id('unchanged')} path="straight" startAnchor={{ position: 'bottom', offset: { x: -60 } }}
      endAnchor={{ position: 'top', offset: { x: 60 } }} labels={{ middle: <Label>input hash = base's</Label> }} />
    <Arrow variant="primary" from={id('extract')} to={id('json')} path="straight" startAnchor="right" endAnchor="left" />
    <Arrow variant="primary" from={id('json')} to={id('digest')} path="straight" startAnchor="bottom" endAnchor="top" />
    <Arrow variant="primary" from={id('digest')} to={id('document')} path="straight" startAnchor="left" endAnchor="right"
      labels={{ middle: <Label>new</Label> }} />
    <Arrow variant="primary" from={id('document')} to={id('postings')} path="straight" startAnchor="left" endAnchor="right" />
    <Arrow variant="secondary" from={id('digest')} to={id('reuse')} path="straight" startAnchor="bottom" endAnchor="top"
      labels={{ middle: <Label>found</Label> }} />
    <Arrow variant="primary" from={id('document')} to={id('binding')} path="straight" startAnchor="bottom" endAnchor="top" />
    <Arrow variant="secondary" from={id('reuse')} to={id('binding')} path="straight" startAnchor="left" endAnchor="right" />
  </>}</Diagram>;
}

export function SymbolTables() {
  return <Diagram className="relative">{(id) => <>
    <div className="grid px-6 py-8" style={{
      gridTemplateColumns: 'repeat(3, auto)', columnGap: '120px', rowGap: '72px', alignItems: 'center', justifyItems: 'center', justifyContent: 'center',
    }}>
      <div style={{ gridColumn: 1, gridRow: 1 }}>
        <Entity id={id('revision')} title="source_revisions" detail="One file version; unique (root_id, path_key, content_hash, package_path, git_path)" minWidth="290px" accent={COLORS.accent} fields={[
          { name: 'id', type: 'uuid', pk: true },
          { name: 'root_id', type: 'uuid · modules.id', fk: true },
          { name: 'path_key', type: 'text' },
          { name: 'content_hash', type: 'text · 64 hex' },
          { name: 'package_path', type: 'text' },
          { name: 'git_path', type: "text · default ''" },
          { name: 'size_bytes', type: 'bigint' },
        ]} />
      </div>
      <div style={{ gridColumn: 2, gridRow: 1 }}>
        <Entity id={id('document')} title="documents" detail="Unique (root_id, path_key, content_digest) and (root_id, path_key, ordinal)" minWidth="310px" fields={[
          { name: 'id', type: 'uuid', pk: true },
          { name: 'root_id', type: 'uuid · modules.id', fk: true },
          { name: 'path_key', type: 'text' },
          { name: 'source_revision_id', type: 'uuid', fk: true },
          { name: 'package_path', type: 'text' },
          { name: 'input_hash', type: 'text · provenance' },
          { name: 'indexer_version', type: 'text' },
          { name: 'coverage', type: 'indexed | partial | syntax | excluded' },
          { name: 'symbol_count', type: 'integer' },
          { name: 'occurrence_count', type: 'integer' },
          { name: 'content', type: 'jsonb · format 11' },
          { name: 'ordinal', type: 'bigint · unique' },
          { name: 'content_digest', type: 'text? · DocumentDigest' },
        ]} indexes={['documents_source_idx (source_revision_id)']} />
      </div>
      <div style={{ gridColumn: 3, gridRow: 1 }}>
        <Entity id={id('postings')} title="symbol_postings" detail="Role 0 definition, 1 reference, 2 implements, 3 embeds, 4 relationship_source, 5 relationship_target" minWidth="320px" fields={[
          { name: 'document_ordinal', type: 'bigint · ordinal', pk: true, fk: true },
          { name: 'symbol_handle', type: 'bigint · handle', pk: true, fk: true },
          { name: 'role', type: 'integer · 0–5', pk: true },
          { name: 'root_ordinal', type: 'integer · modules.ordinal', fk: true },
          { name: 'occurrence_count', type: 'integer ≥ 1' },
        ]} indexes={['symbol_postings_symbol_idx (symbol_handle, role, root_ordinal, document_ordinal)']} />
      </div>
      <div className="flex flex-col items-center" style={{ gridColumn: 1, gridRow: 2, gap: '56px' }}>
        <Entity id={id('modules')} title="symbol_modules" detail="Numbers the handle's 11-bit module field" minWidth="250px" accent={COLORS.accent} fields={[
          { name: 'number', type: 'integer · 0–2047', pk: true },
          { name: 'module_key', type: 'text · unique' },
        ]} />
        <Entity id={id('packages')} title="symbol_packages" detail="Numbers the handle's 16-bit package field" minWidth="250px" accent={COLORS.accent} fields={[
          { name: 'module_number', type: 'integer', pk: true, fk: true },
          { name: 'number', type: 'integer · 0–65535', pk: true },
          { name: 'package_path', type: 'text · unique per module' },
        ]} />
      </div>
      <div style={{ gridColumn: 2, gridRow: 2 }}>
        <Entity id={id('kinds')} title="symbol_kinds" detail="Kind registry; code packs into the handle's 6-bit kind field" minWidth="290px" accent={COLORS.accent} fields={[
          { name: 'code', type: 'smallint · 1–63', pk: true },
          { name: 'name', type: 'text · unique' },
          { name: 'category', type: 'container | type | callable | member' },
          { name: 'namespace', type: 'text' },
        ]} />
      </div>
      <div style={{ gridColumn: 3, gridRow: 2 }}>
        <Entity id={id('symbols')} title="symbols" detail="Unique canonical_key and handle; the handle packs registry numbers without a foreign key" minWidth="320px" fields={[
          { name: 'id', type: 'text · 64 hex SHA-256', pk: true },
          { name: 'identity_version', type: 'integer' },
          { name: 'canonical_key', type: 'text · unique' },
          { name: 'module_key', type: 'text' },
          { name: 'package_path', type: 'text' },
          { name: 'kind', type: 'text · symbol_kinds.name', fk: true },
          { name: 'owner_id', type: 'text? · symbols.id', fk: true },
          { name: 'name', type: 'text' },
          { name: 'search_name', type: 'text' },
          { name: 'visibility', type: 'exported | internal' },
          { name: 'parameter_types', type: 'jsonb' },
          { name: 'handle', type: 'bigint · unique · H64b' },
        ]} indexes={['symbols_lookup_idx (module_key, package_path, kind, owner_id, name)', 'symbols_search_idx (search_name, module_key, id)']} />
      </div>
    </div>
    <Arrow variant="er" from={id('document')} to={id('revision')} path="straight" startAnchor="left" endAnchor="right"
      labels={{ middle: <Label>N:1 revision</Label> }} />
    <Arrow variant="er" from={id('postings')} to={id('document')} path="straight" startAnchor="left" endAnchor="right"
      labels={{ middle: <Label>N:1 document</Label> }} />
    <Arrow variant="er" from={id('postings')} to={id('symbols')} path="straight" startAnchor="bottom" endAnchor="top"
      labels={{ middle: <Label>N:1 handle</Label> }} />
    <Arrow variant="er" from={id('symbols')} to={id('kinds')} path="straight" startAnchor="left" endAnchor="right"
      labels={{ middle: <Label>N:1 kind name</Label> }} />
    <Arrow variant="er" from={id('packages')} to={id('modules')} path="straight" startAnchor="top" endAnchor="bottom"
      labels={{ middle: <Label>N:1 module</Label> }} />
  </>}</Diagram>;
}

function KeyRow({ name, type, gap, retired }: { name: string; type: string; gap?: number; retired?: boolean }) {
  return <div className="flex items-center gap-1.5 py-0.5">
    <span className={`font-semibold ${mono} ${retired ? 'line-through' : ''}`} style={{ color: retired ? COLORS.muted : COLORS.accent }}>{name}</span>
    <span className={`ml-auto text-[8.5pt] ${retired ? 'line-through' : ''}`} style={{ color: COLORS.muted }}>{type}</span>
    {gap !== undefined && <GapMarker gap={gap} />}
  </div>;
}

function Note({ children, gap }: { children: React.ReactNode; gap?: number }) {
  return <div className="mt-1 flex items-start gap-1.5 border-t pt-1 text-[8.5pt] leading-snug" style={{ borderColor: COLORS.background, color: COLORS.muted }}>
    <span>{children}</span>
    {gap !== undefined && <GapMarker gap={gap} />}
  </div>;
}

export function SymbolStorageEndState() {
  return <Diagram className="relative">{(id) => <>
    <div className="grid px-6 py-8" style={{
      gridTemplateColumns: 'repeat(3, 280px)', columnGap: '120px', rowGap: '72px', alignItems: 'center', justifyItems: 'start',
    }}>
      <div style={{ gridColumn: 1, gridRow: 1 }}>
        <StatusBox id={id('producer')} title="Producer publication" status="planned" gap={7} minWidth="280px">
          <KeyRow name="PublishedSymbol.Key" type="publication-local" />
          <KeyRow name="OwnerKey" type="another supplied symbol" />
          <Note>Storage resolves the natural key and allocates <Code>symbols.id</Code>; producers no longer hash ids.</Note>
        </StatusBox>
      </div>
      <div style={{ gridColumn: 2, gridRow: 1 }}>
        <StatusBox id={id('symbols')} title="symbols" status="built" gap={1} minWidth="280px">
          <KeyRow name="id" type="bigint · PK · the handle value" gap={1} />
          <KeyRow name="canonical_key" type="text · unique natural key" />
          <KeyRow name="owner_id" type="bigint? · symbols.id" gap={1} />
          <Note gap={6}>Permanent registry: prune and reindex keep every committed row.</Note>
        </StatusBox>
      </div>
      <div style={{ gridColumn: 3, gridRow: 1 }}>
        <StatusBox id={id('hash')} title="Hashed id and handle" status="retired" gap={1} minWidth="280px">
          <KeyRow name="symbols.id" type="text · 64 hex SHA-256" retired />
          <KeyRow name="symbols.handle" type="bigint · unique" retired />
          <KeyRow name="identity_version" type="integer" retired />
        </StatusBox>
      </div>

      <div style={{ gridColumn: 1, gridRow: 2 }}>
        <StatusBox id={id('locations')} title="symbol_locations" status="planned" gap={2} minWidth="280px">
          <KeyRow name="version_id" type="PK · documents.ordinal" />
          <KeyRow name="declaration_index" type="PK · integer" />
          <KeyRow name="symbol_id" type="bigint? · symbols.id" />
          <KeyRow name="source_revision_id" type="uuid" />
          <KeyRow name="name_start_offset" type="bigint" />
          <KeyRow name="position" type="BYTEA · compact" gap={5} />
        </StatusBox>
      </div>
      <div style={{ gridColumn: 2, gridRow: 2 }}>
        <StatusBox id={id('postings')} title="symbol_postings" status="retired" gap={4} minWidth="280px">
          <KeyRow name="document_ordinal" type="PK" retired />
          <KeyRow name="symbol_handle" type="PK" retired />
          <KeyRow name="role" type="PK · 0–5" retired />
          <KeyRow name="occurrence_count" type="integer" retired />
          <Note>With <Code>DefinitionPostings</Code> and the posting roles.</Note>
        </StatusBox>
      </div>
      <div style={{ gridColumn: 3, gridRow: 2 }}>
        <StatusBox id={id('references')} title="symbol_references" status="planned" gap={3} minWidth="280px">
          <KeyRow name="version_id" type="PK · documents.ordinal" />
          <KeyRow name="reference_index" type="PK · integer" />
          <KeyRow name="from_symbol_id" type="bigint? · caller" />
          <KeyRow name="to_symbol_id" type="bigint? · target" />
          <KeyRow name="kind" type="integer · 0–7" />
          <KeyRow name="start_offset" type="bigint" />
          <KeyRow name="position" type="BYTEA · compact" gap={5} />
        </StatusBox>
      </div>

      <div style={{ gridColumn: 1, gridRow: 3 }}>
        <StatusBox id={id('revisions')} title="source_revisions" status="built" minWidth="280px">
          <KeyRow name="id" type="uuid · PK" />
          <KeyRow name="path_key" type="text" />
          <KeyRow name="content_hash" type="text · 64 hex" />
        </StatusBox>
      </div>
      <div style={{ gridColumn: 2, gridRow: 3 }}>
        <StatusBox id={id('documents')} title="documents" status="built" minWidth="280px">
          <KeyRow name="ordinal" type="bigint · the version_id" />
          <KeyRow name="content" type="jsonb · ids as decimal strings" gap={8} />
          <Note>Kept for rich payloads, shapes, diagnostics and historical diffs.</Note>
          <Note gap={9}>Go documents may become compact per-file resolution records, once the plan's latency gate is met.</Note>
        </StatusBox>
      </div>
      <div style={{ gridColumn: 3, gridRow: 3 }}>
        <StatusBox id={id('status')} title="document_projection_status" status="planned" gap={5} minWidth="280px">
          <KeyRow name="version_id" type="PK · documents.ordinal" />
          <KeyRow name="projection_version" type="integer · 1" />
          <KeyRow name="declaration_count" type="integer" />
          <KeyRow name="occurrence_count" type="integer · kinds 0–5" />
          <KeyRow name="relationship_count" type="integer · kinds 6–7" />
        </StatusBox>
      </div>
    </div>
    <Arrow variant="er" from={id('producer')} to={id('symbols')} path="straight" startAnchor="right" endAnchor="left" />
    <Arrow variant="er" from={id('hash')} to={id('symbols')} path="straight" startAnchor="left" endAnchor="right"
      color={COLORS.muted} labels={{ middle: <Label>handle → id</Label> }} />
    <Arrow variant="er" from={id('locations')} to={id('symbols')} path="straight" startAnchor="top"
      endAnchor={{ position: 'bottom', offset: { x: -80 } }} />
    <Arrow variant="er" from={id('references')} to={id('symbols')} path="straight" startAnchor="top"
      endAnchor={{ position: 'bottom', offset: { x: 80 } }} />
    <Arrow variant="er" from={id('postings')} to={id('symbols')} path="straight" startAnchor="top" endAnchor="bottom" color={COLORS.muted} />
    <Arrow variant="er" from={id('postings')} to={id('documents')} path="straight" startAnchor="bottom" endAnchor="top" color={COLORS.muted} />
    <Arrow variant="er" from={id('locations')} to={id('revisions')} path="straight" startAnchor={{ position: 'bottom', offset: { x: -40 } }}
      endAnchor={{ position: 'top', offset: { x: -40 } }} />
    <Arrow variant="er" from={id('locations')} to={id('documents')} path="straight" startAnchor={{ position: 'bottom', offset: { x: 80 } }}
      endAnchor={{ position: 'top', offset: { x: -80 } }} />
    <Arrow variant="er" from={id('references')} to={id('documents')} path="straight" startAnchor={{ position: 'bottom', offset: { x: -80 } }}
      endAnchor={{ position: 'top', offset: { x: 80 } }} />
    <Arrow variant="er" from={id('status')} to={id('documents')} path="straight" startAnchor="left" endAnchor="right" />
  </>}</Diagram>;
}

function LaneTitle({ children }: { children: React.ReactNode }) {
  return <div className="border-b pb-1 text-[10pt] font-bold" style={{ borderColor: COLORS.primary, color: COLORS.accent }}>{children}</div>;
}

export function LookupTodayVsEndState() {
  return <Diagram className="relative">{(id) => <>
    <div className="grid px-6 py-8" style={{
      gridTemplateColumns: 'repeat(5, 186px)', columnGap: '60px', alignItems: 'center', justifyItems: 'start',
    }}>
      <div style={{ gridColumn: '1 / span 5', gridRow: 1, justifySelf: 'stretch' }}>
        <LaneTitle>Today: who calls Store.Save?</LaneTitle>
      </div>
      <div style={{ gridColumn: '1 / span 5', gridRow: 2, height: '20px' }} />
      <div style={{ gridColumn: 1, gridRow: 3 }}>
        <StatusBox id={id('today-symbols')} title="symbols" status="built" minWidth="186px">
          <Text>Match <Code>search_name</Code> and <Code>name</Code>, name the owner chain, take the row's SHA-256 id and handle.</Text>
        </StatusBox>
      </div>
      <div style={{ gridColumn: 2, gridRow: 3 }}>
        <StatusBox id={id('today-defined')} title="DefinedSymbols" status="built" minWidth="186px">
          <Text>Is the handle defined in the selected snapshot? <Code>symbol_deltas</Code> along the base chain.</Text>
        </StatusBox>
      </div>
      <div style={{ gridColumn: 3, gridRow: 3 }}>
        <StatusBox id={id('today-postings')} title="symbol_postings" status="retired" gap={4} minWidth="186px">
          <Text>Range scan by handle, role reference: document ordinals and counts only, kept when the document is active.</Text>
        </StatusBox>
      </div>
      <div style={{ gridColumn: 4, gridRow: 3 }}>
        <StatusBox id={id('today-decode')} title="DecodeDocument" status="built" minWidth="186px">
          <Text>Read each listed document's JSON, in batches of 256, and decode it.</Text>
          <Note gap={3}>Kept in the end state, but this query no longer needs it.</Note>
        </StatusBox>
      </div>
      <div style={{ gridColumn: 5, gridRow: 3 }}>
        <StatusBox id={id('today-result')} title="Call sites" status="built" minWidth="186px">
          <Text>Scan every occurrence; keep symbol Save with role <Code>call</Code>. Site from its range, caller from <Code>enclosing</Code>.</Text>
        </StatusBox>
      </div>

      <div style={{ gridColumn: '1 / span 5', gridRow: 4, height: '40px' }} />
      <div style={{ gridColumn: '1 / span 5', gridRow: 5, justifySelf: 'stretch' }}>
        <LaneTitle>End state: the same question</LaneTitle>
      </div>
      <div style={{ gridColumn: '1 / span 5', gridRow: 6, height: '20px' }} />
      <div style={{ gridColumn: 1, gridRow: 7 }}>
        <StatusBox id={id('end-symbols')} title="symbols" status="built" gap={1} minWidth="186px">
          <Text>The same match; <Code>id</Code> is the bigint, the only symbol id.</Text>
        </StatusBox>
      </div>
      <div style={{ gridColumn: 2, gridRow: 7 }}>
        <StatusBox id={id('end-defined')} title="DefinedSymbols" status="built" minWidth="186px">
          <Text>Unchanged: existence still comes from <Code>symbol_deltas</Code>.</Text>
        </StatusBox>
      </div>
      <div style={{ gridColumn: 3, gridRow: 7 }}>
        <StatusBox id={id('end-references')} title="symbol_references" status="planned" gap={3} minWidth="186px">
          <Text>The to index: <Code>to_symbol_id</Code>, kind 2 call, and only the active versions.</Text>
        </StatusBox>
      </div>
      <div style={{ gridColumn: 5, gridRow: 7 }}>
        <StatusBox id={id('end-result')} title="Call sites" status="built" minWidth="186px">
          <Text>Each row carries the site (<Code>start_offset</Code>, <Code>position</Code>) and the caller (<Code>from_symbol_id</Code>).</Text>
          <Note gap={3}>Planned columns of symbol_references.</Note>
        </StatusBox>
      </div>
    </div>
    <Arrow variant="primary" from={id('today-symbols')} to={id('today-defined')} path="straight" startAnchor="right" endAnchor="left" />
    <Arrow variant="primary" from={id('today-defined')} to={id('today-postings')} path="straight" startAnchor="right" endAnchor="left" />
    <Arrow variant="primary" from={id('today-postings')} to={id('today-decode')} path="straight" startAnchor="right" endAnchor="left" />
    <Arrow variant="primary" from={id('today-decode')} to={id('today-result')} path="straight" startAnchor="right" endAnchor="left" />
    <Arrow variant="primary" from={id('end-symbols')} to={id('end-defined')} path="straight" startAnchor="right" endAnchor="left" />
    <Arrow variant="primary" from={id('end-defined')} to={id('end-references')} path="straight" startAnchor="right" endAnchor="left" />
    <Arrow variant="primary" from={id('end-references')} to={id('end-result')} path="straight" startAnchor="right" endAnchor="left"
      labels={{ middle: <Label>no document decoded</Label> }} />
  </>}</Diagram>;
}

const PROPOSAL = 'docs/symbol-reference-storage.md';

export const SYMBOL_STORAGE_GAPS: Gap[] = [
  {
    id: 1, title: 'One bigint symbol id', status: 'proposed',
    today: 'symbols.id is the hex SHA-256 of the canonical key and symbols.handle the H64b bigint; postings, symbol and implements deltas, declaration metrics and measures store the handle, documents, owner_id and the API carry the hex id, and readers translate one to the other.',
    endState: 'symbols.id is a bigint primary key taking each row\'s existing handle value, and owner_id a bigint self-reference; the SHA-256 id, the handle column and identity_version are dropped, and symbol_deltas.symbol_handle is renamed symbol_id (Deltas page). The proposal says every symbol key uses the one id but does not name the other columns whose foreign keys reference symbols.handle and need retargeting: implements_deltas.type_handle and interface_handle, declaration_metrics.symbol_handle and declaration_measures.symbol_handle.',
    source: `${PROPOSAL} §Decision and purpose, §Permanent symbol IDs, §symbols`, evidence: 'storage/migrations/06_symbol_index.hcl:1-80; 17_implements.hcl:44-55; 19_metrics.hcl:48-51; 22_producer_metrics.hcl:38-41',
  },
  {
    id: 2, title: 'symbol_locations', status: 'proposed',
    today: 'declaration_metrics (migration 19) already holds one row per declaration, unproven ones included, keyed (document_ordinal, symbol_key) with a nullable symbol_handle, its name range and a (symbol_handle, document_ordinal) index; only SQL queries that select metrics without properties read it instead of decoding. Extents, byte spans and a source-position index exist only in document JSON.',
    endState: 'One row per saved declaration entry, keyed (version_id, declaration_index), with symbol_id, source_revision_id, name_start_offset, a compact name and extent record, and indexes for symbol and file-position lookup.',
    source: `${PROPOSAL} §symbol_locations`, evidence: 'storage/migrations/19_metrics.hcl:18-61; indexer/document_metrics.go:33; query/sql.go:158; no symbol_locations table',
  },
  {
    id: 3, title: 'symbol_references', status: 'proposed',
    today: 'A posting says only that a document mentions a symbol; callers and references decode every listed document and scan its occurrences for the site, kind and enclosing caller.',
    endState: 'One row per occurrence, plus implements and embeds rows, with from_symbol_id, to_symbol_id, kind 0–7 and the site; from and to indexes answer graph and reference queries with zero document decodes.',
    source: `${PROPOSAL} §symbol_references, §Indexed readers`, evidence: 'query/module_index_ops.go:79-103',
  },
  {
    id: 4, title: 'Retire symbol_postings', status: 'proposed',
    today: 'symbol_postings (document_ordinal, symbol_handle, role 0–5, occurrence_count) is written at publication, read directly by queries and prune, and by symboldiff through DefinitionPostings.',
    endState: 'The table, SymbolPosting, the posting roles and DefinitionPostings are removed, with no compatibility view; declaration-location reads replace them.',
    source: `${PROPOSAL} §Pruning and removed interfaces`, evidence: 'storage/migrations/06_symbol_index.hcl:173-219; storage/diff_support.go:22',
  },
  {
    id: 5, title: 'Compact positions and projection status', status: 'proposed',
    today: 'No projection tables exist, so there is no position codec and no record of which documents have been projected.',
    endState: 'position BYTEA/BLOB records (header byte plus LEB128 and ZigZag varints) beside an indexed start offset, and document_projection_status per document, written after the rows validate, zero counts included.',
    source: `${PROPOSAL} §Compact position encoding, §document_projection_status`, evidence: 'storage/migrations (no such table or column)',
  },
  {
    id: 6, title: 'Permanent symbol registry', status: 'proposed',
    today: 'Prune deletes symbols no posting or delta names. A new symbol takes one past the largest handle still stored in its bucket, so after a prune a handle value can be allocated again, to the same symbol or to a different one. A database of an older generation (pre-handle, H64a or two-layer fingerprints) has every index table dropped, symbols and its number registries included.',
    endState: 'Every committed symbols row and owner mapping survives prune and reindex; symbol_modules, symbol_packages, symbol_kinds and symbol_layout are kept, and an incompatible layout needs an explicit upgrade.',
    source: `${PROPOSAL} §Permanent symbol IDs, §Pruning and removed interfaces`, evidence: 'storage/prune_symbols.go:13; storage/symbol_handles.go:123-136; storage/handle_cutover.go:25-50',
  },
  {
    id: 7, title: 'Storage allocates producer ids', status: 'proposed',
    today: 'A non-Go producer computes each symbol\'s id with SymbolID(Identity) and writes those hex ids into its documents.',
    endState: 'Producers send PublishedSymbol.Key and OwnerKey local to one publication; storage resolves natural keys, allocates ids in the publication transaction and rewrites the references.',
    source: `${PROPOSAL} §Producer references and allocation`, evidence: 'indexer/external_types.go:74-79; indexer/symbol_id.go:63',
  },
  {
    id: 8, title: 'Decimal-string ids in JSON and the API', status: 'proposed',
    today: 'DocumentSymbol.id, occurrence symbol and enclosing, selectors and API results carry 64-hex strings.',
    endState: 'Canonical decimal strings of the bigint id in document JSON, the API, selectors and graph node ids, never JavaScript numbers. The proposal calls this document format 4; documents are already at format 11.',
    source: `${PROPOSAL} §Retained document and API IDs`, evidence: 'storage/document.go:18, 64, 116, 120',
  },
  {
    id: 9, title: 'Per-file resolution records', status: 'open',
    today: 'Go documents hold full JSON. The prototype in hack/dochydration measured records at 3 % of the size; the batch source reader it waited for, Pipeline.ReadModuleSources, has landed.',
    endState: 'Compact per-file resolution records replace Go documents, adoption gated on the plan\'s condition: "Adopt records on that basis, after the batch source reader lands and cold per-package latency is re-measured." The reference-storage proposal still keeps document JSON for rich payloads.',
    source: 'docs/incremental-snapshot-plan.md §Adversarial review: are documents necessary?', evidence: 'hack/dochydration; query/module_sources.go:22',
  },
];

export function SymbolStorageNotes() {
  return <ul className="flex list-disc flex-col gap-1 pl-4 text-[9pt] leading-snug" style={{ color: COLORS.muted }}>
    <li>Not drawn as a gap: the proposal selects documents by <Code>(root_id, path_key, input_hash)</Code> through a covering metadata index. Migration 18 replaced that with <Code>document_deltas</Code>, and documents are keyed by <Code>content_digest</Code>; <Code>input_hash</Code> is provenance only.</li>
    <li>The proposal derives kind 6 implements rows from each declaration's <Code>Implements</Code> array. Since document format 6, documents carry none: implements facts are snapshot-level, in <Code>implements_deltas</Code>.</li>
    <li>The proposal does not say what happens to <Code>declaration_metrics</Code> (migration 19), which already projects each declaration's name range and handle, once <Code>symbol_locations</Code> exists.</li>
  </ul>;
}

export default function SymbolStoragePage() {
  return <Page>
    <Section title="Where, when and what">
      <p>Storage has three layers. Modules and their checkouts say where; immutable snapshots, stored as deltas against a base, say when; the immutable rows below are shared by every snapshot that has them.</p>
      <StorageLayers />
    </Section>
    <Section title="Symbol identity">
      <p>A symbol's id is the SHA-256 of its canonical key. The same row also gets a compact H64b handle, packed from registry numbers, which postings and deltas store.</p>
      <SymbolIdentity />
    </Section>
    <Section title="From a file to stored facts">
      <p>One file becomes a source revision, a document and its postings. A package whose input hash equals its base's keeps its documents without rendering; a rendered document whose digest is already stored is reused.</p>
      <FileToFacts />
    </Section>
    <Section title="Symbol tables">
      <p>Columns and keys from migrations 05, 06, 07 and 10.</p>
      <SymbolTables />
    </Section>
    <Section title="End state">
      <p>What docs/symbol-reference-storage.md commits to, against what is built.</p>
      <Legend />
      <SymbolStorageEndState />
    </Section>
    <Section title="Who calls Store.Save?">
      <p>The same question today and in the end state.</p>
      <Legend />
      <LookupTodayVsEndState />
    </Section>
    <Section title="Gaps">
      <GapList gaps={SYMBOL_STORAGE_GAPS} />
      <SymbolStorageNotes />
    </Section>
  </Page>;
}

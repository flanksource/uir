// @live
import React from 'react';
import { Arrow, BoxNode, COLORS, Diagram, Page, Section } from '@flanksource/facet';

type Marker = 'PK' | 'FK' | 'IX';

interface FieldDef {
  name: string;
  type: string;
  markers?: Marker[];
}

interface EntityBoxProps {
  id: string;
  title: string;
  description: string;
  fields: FieldDef[];
  constraints: string[];
  accent?: string;
  minWidth?: string;
}

interface DetailCardDef {
  title: string;
  summary: string;
  items: string[];
  accent?: string;
}

interface ReviewFinding {
  scenario: string;
  failure: string;
  control: string;
}

type EntityDefinition = Omit<EntityBoxProps, 'id'>;
type IdFactory = (name: string) => string;

const markerColors: Record<Marker, string> = {
  PK: COLORS.pk,
  FK: COLORS.fk,
  IX: COLORS.muted,
};

const projectEntity: EntityDefinition = {
  title: 'uir_projects',
  description: 'A logical analysis boundary; one project may contain many independent roots.',
  minWidth: '58mm',
  accent: COLORS.accent,
  fields: [
    { name: 'id', type: 'ID', markers: ['PK'] },
    { name: 'project_key', type: 'String', markers: ['IX'] },
    { name: 'name', type: 'String' },
    { name: 'properties', type: 'JSON' },
    { name: 'created_at', type: 'Timestamp' },
    { name: 'updated_at', type: 'Timestamp' },
  ],
  constraints: ['UQ (project_key)', 'No checkout path participates in identity'],
};

const snapshotEntity: EntityDefinition = {
  title: 'uir_snapshots',
  description: 'An immutable, point-in-time analysis of the complete project root set.',
  minWidth: '62mm',
  fields: [
    { name: 'id', type: 'ID', markers: ['PK'] },
    { name: 'project_id', type: 'ID', markers: ['FK', 'IX'] },
    { name: 'state', type: 'String', markers: ['IX'] },
    { name: 'revision_set_hash', type: 'String', markers: ['IX'] },
    { name: 'configuration_hash', type: 'String' },
    { name: 'extractor_version', type: 'String' },
    { name: 'payload_schema', type: 'String' },
    { name: 'document_payload', type: 'JSON' },
    { name: 'started_at', type: 'Timestamp' },
    { name: 'completed_at', type: 'Timestamp?' },
    { name: 'properties', type: 'JSON' },
  ],
  constraints: ['state ∈ building | ready | failed', 'UQ (project_id, id)'],
};

const projectHeadEntity: EntityDefinition = {
  title: 'uir_project_heads',
  description: 'One portable publication pointer; avoids a cyclic project-to-snapshot schema.',
  minWidth: '62mm',
  accent: COLORS.outputBorder,
  fields: [
    { name: 'project_id', type: 'ID', markers: ['PK', 'FK'] },
    { name: 'snapshot_id', type: 'ID', markers: ['FK', 'IX'] },
    { name: 'version', type: 'Int64' },
    { name: 'activated_at', type: 'Timestamp' },
  ],
  constraints: [
    'UQ (snapshot_id)',
    'FK (project_id, snapshot_id) → snapshots(project_id, id)',
    'Compare-and-swap on version',
  ],
};

const rootEntity: EntityDefinition = {
  title: 'uir_roots',
  description: 'A Git root, nested repository, directory, or virtual namespace in one snapshot.',
  minWidth: '66mm',
  accent: COLORS.fk,
  fields: [
    { name: 'id', type: 'ID', markers: ['PK'] },
    { name: 'snapshot_id', type: 'ID', markers: ['FK', 'IX'] },
    { name: 'parent_root_id', type: 'ID?', markers: ['FK', 'IX'] },
    { name: 'root_key', type: 'String', markers: ['IX'] },
    { name: 'kind', type: 'String', markers: ['IX'] },
    { name: 'mount_path', type: 'String', markers: ['IX'] },
    { name: 'repository_key', type: 'String?', markers: ['IX'] },
    { name: 'repository_uri', type: 'String?' },
    { name: 'revision', type: 'String?', markers: ['IX'] },
    { name: 'content_set_hash', type: 'String', markers: ['IX'] },
    { name: 'submodule_path', type: 'String?' },
    { name: 'path_case', type: 'String' },
    { name: 'normalization_version', type: 'String' },
    { name: 'local_path', type: 'String?' },
    { name: 'properties', type: 'JSON' },
  ],
  constraints: [
    'UQ (snapshot_id, id)',
    'UQ (snapshot_id, root_key)',
    'UQ (snapshot_id, mount_path)',
    'FK (snapshot_id, parent_root_id) → roots(snapshot_id, id)',
  ],
};

const sourceEntity: EntityDefinition = {
  title: 'uir_sources',
  description: 'One file or virtual source, scoped to exactly one resolved root boundary.',
  minWidth: '64mm',
  fields: [
    { name: 'id', type: 'ID', markers: ['PK'] },
    { name: 'root_id', type: 'ID', markers: ['FK', 'IX'] },
    { name: 'path_key', type: 'String', markers: ['IX'] },
    { name: 'display_path', type: 'String' },
    { name: 'kind', type: 'String', markers: ['IX'] },
    { name: 'language', type: 'String', markers: ['IX'] },
    { name: 'content_hash', type: 'String', markers: ['IX'] },
    { name: 'size_bytes', type: 'Int64' },
    { name: 'modified_at', type: 'Timestamp?' },
    { name: 'properties', type: 'JSON' },
  ],
  constraints: [
    'UQ (root_id, id)',
    'UQ (root_id, path_key)',
    'path_key is normalized, root-relative, and traversal-free',
  ],
};

const nodeEntity: EntityDefinition = {
  title: 'uir_nodes',
  description: 'Canonical symbol identity within one root, plus lossless typed payload.',
  minWidth: '70mm',
  fields: [
    { name: 'id', type: 'ID', markers: ['PK'] },
    { name: 'snapshot_id', type: 'ID', markers: ['FK', 'IX'] },
    { name: 'root_id', type: 'ID', markers: ['FK', 'IX'] },
    { name: 'parent_id', type: 'ID?', markers: ['FK', 'IX'] },
    { name: 'child_slot', type: 'String', markers: ['IX'] },
    { name: 'ordinal', type: 'Int' },
    { name: 'node_type', type: 'String', markers: ['IX'] },
    { name: 'identity_key', type: 'String', markers: ['IX'] },
    { name: 'symbol_key', type: 'String', markers: ['IX'] },
    { name: 'module', type: 'String', markers: ['IX'] },
    { name: 'package', type: 'String', markers: ['IX'] },
    { name: 'type_name', type: 'String', markers: ['IX'] },
    { name: 'method', type: 'String', markers: ['IX'] },
    { name: 'field', type: 'String', markers: ['IX'] },
    { name: 'signature', type: 'String' },
    { name: 'language', type: 'String', markers: ['IX'] },
    { name: 'traits', type: 'JSON' },
    { name: 'payload_schema', type: 'String' },
    { name: 'payload', type: 'JSON' },
    { name: 'semantic_hash', type: 'String', markers: ['IX'] },
    { name: 'created_at', type: 'Timestamp' },
  ],
  constraints: [
    'UQ (snapshot_id, id)',
    'UQ (root_id, id)',
    'UQ (snapshot_id, root_id, id)',
    'UQ (root_id, identity_key)',
    'FK (snapshot_id, root_id) → roots(snapshot_id, id)',
    'FK (root_id, parent_id) → nodes(root_id, id)',
  ],
};

const nodeLocationEntity: EntityDefinition = {
  title: 'uir_node_locations',
  description: 'Many-to-many provenance for declarations assembled from one or more sources.',
  minWidth: '66mm',
  accent: COLORS.outputBorder,
  fields: [
    { name: 'id', type: 'ID', markers: ['PK'] },
    { name: 'root_id', type: 'ID', markers: ['FK', 'IX'] },
    { name: 'node_id', type: 'ID', markers: ['FK', 'IX'] },
    { name: 'source_id', type: 'ID', markers: ['FK', 'IX'] },
    { name: 'role', type: 'String', markers: ['IX'] },
    { name: 'ordinal', type: 'Int' },
    { name: 'start_line', type: 'Int?' },
    { name: 'end_line', type: 'Int?' },
    { name: 'column', type: 'Int?' },
    { name: 'is_primary', type: 'Bool' },
  ],
  constraints: [
    'UQ (node_id, source_id, role, ordinal)',
    'FK (root_id, node_id) → nodes(root_id, id) · CASCADE',
    'FK (root_id, source_id) → sources(root_id, id) · CASCADE',
  ],
};

const fieldEntity: EntityDefinition = {
  title: 'uir_fields',
  description: 'Optional field or column detail keyed one-to-one by its canonical node.',
  minWidth: '70mm',
  accent: COLORS.outputBorder,
  fields: [
    { name: 'node_id', type: 'ID', markers: ['PK', 'FK'] },
    { name: 'role', type: 'String', markers: ['IX'] },
    { name: 'ordinal', type: 'Int?' },
    { name: 'label', type: 'String' },
    { name: 'field_type', type: 'String', markers: ['IX'] },
    { name: 'native_type', type: 'String' },
    { name: 'max_length', type: 'Int?' },
    { name: 'precision', type: 'Int?' },
    { name: 'scale', type: 'Int?' },
    { name: 'enum_values', type: 'JSON' },
    { name: 'type_ref', type: 'JSON' },
    { name: 'default_value', type: 'JSON' },
    { name: 'validation', type: 'JSON' },
    { name: 'visibility', type: 'String' },
    { name: 'is_nullable', type: 'Bool' },
    { name: 'is_primary_key', type: 'Bool' },
    { name: 'is_auto_increment', type: 'Bool' },
    { name: 'is_unique', type: 'Bool' },
    { name: 'is_read_only', type: 'Bool' },
    { name: 'is_write_only', type: 'Bool' },
  ],
  constraints: ['node_id → uir_nodes.id · ON DELETE CASCADE'],
};

const relationshipEntity: EntityDefinition = {
  title: 'uir_relationships',
  description: 'Edges in one snapshot; unresolved or external targets retain scoped locators.',
  minWidth: '74mm',
  accent: COLORS.fk,
  fields: [
    { name: 'id', type: 'ID', markers: ['PK'] },
    { name: 'snapshot_id', type: 'ID', markers: ['FK', 'IX'] },
    { name: 'from_root_id', type: 'ID', markers: ['FK', 'IX'] },
    { name: 'from_node_id', type: 'ID', markers: ['FK', 'IX'] },
    { name: 'to_snapshot_id', type: 'ID?', markers: ['FK'] },
    { name: 'to_node_id', type: 'ID?', markers: ['FK', 'IX'] },
    { name: 'edge_key', type: 'String', markers: ['IX'] },
    { name: 'to_project_key', type: 'String?', markers: ['IX'] },
    { name: 'to_root_key', type: 'String?', markers: ['IX'] },
    { name: 'to_identity_key', type: 'String', markers: ['IX'] },
    { name: 'to_symbol_key', type: 'String', markers: ['IX'] },
    { name: 'to_identifier', type: 'JSON' },
    { name: 'relationship_type', type: 'String', markers: ['IX'] },
    { name: 'source_id', type: 'ID?', markers: ['FK', 'IX'] },
    { name: 'start_line', type: 'Int?' },
    { name: 'end_line', type: 'Int?' },
    { name: 'column', type: 'Int?' },
    { name: 'statement_path', type: 'String' },
    { name: 'text', type: 'String' },
    { name: 'payload', type: 'JSON' },
  ],
  constraints: [
    'UQ (from_node_id, edge_key)',
    'FK (snapshot_id, from_root_id, from_node_id) · CASCADE',
    'FK (from_root_id, source_id) → sources(root_id, id)',
    'FK (to_snapshot_id, to_node_id) · SET NULL',
    'CHECK target pair both NULL or same-snapshot and complete',
    'CHECK to_identity_key is not empty',
  ],
};

const storageCards: DetailCardDef[] = [
  {
    title: 'Identity boundaries',
    summary: 'Every identity is explicitly scoped; host filesystem paths never define durable identity.',
    items: [
      'project_key names the logical analysis boundary across machines and checkouts.',
      'root_key is stable for one mounted root across snapshots; identity_key is unique only inside that root.',
      'identity_key hashes canonical structured Identifier fields; symbol_key remains a searchable display projection.',
      'IDs are application-generated UUIDs. Never depend on PostgreSQL-only UUID defaults.',
      'The row ID is storage identity; an explicit UIR Identifier.Id stays inside the canonical identity and payload.',
      'A duplicate (root_id, identity_key) is an extraction error and fails the snapshot.',
    ],
    accent: COLORS.accent,
  },
  {
    title: 'Git roots and submodules',
    summary: 'Each repository boundary keeps its own origin, revision, and mount point.',
    items: [
      'A monorepo is one root unless explicit nested Git boundaries are discovered.',
      'A submodule or nested repository is a child root, not a directory folded into its parent.',
      'The same remote mounted twice receives two root_key values; repository_key may still match.',
      'content_set_hash covers dirty worktrees; revision alone never claims the analyzed bytes.',
      'revision_set_hash sorts root_key, revision, and content_set_hash; database row IDs never affect it.',
      'local_path is diagnostic only so worktrees and relocated clones preserve logical identity.',
    ],
    accent: COLORS.fk,
  },
  {
    title: 'Source paths',
    summary: 'Path identity is portable and deterministic within a root.',
    items: [
      'path_key uses slash separators, is root-relative, and rejects empty, absolute, dot-dot, or escaping paths.',
      'path_case and normalization_version make case behavior explicit instead of inheriting the current host.',
      'A source is assigned to the deepest discovered root, preventing parent and submodule duplication.',
      'Virtual roots use redacted canonical URIs and never persist credentials in path or properties.',
    ],
  },
  {
    title: 'Multi-file nodes',
    summary: 'Node ownership and source provenance are separate concerns.',
    items: [
      'uir_nodes belongs to one root even when a type is assembled from multiple files.',
      'uir_node_locations records every declaration or definition span and one optional primary span.',
      'Moving a source changes locations in the next immutable snapshot without changing the logical symbol key.',
      'Fields and columns remain nodes; uir_fields stores only their queryable structured detail.',
    ],
    accent: COLORS.outputBorder,
  },
  {
    title: 'Snapshot publication',
    summary: 'Readers never observe a partially replaced project graph.',
    items: [
      'Create a building snapshot, then insert roots, sources, nodes, locations, details, and edges.',
      'Validate scope invariants and counts before changing state to ready.',
      'Publish by compare-and-swap updating uir_project_heads in the same transaction.',
      'Failed snapshots remain invisible; retention removes only snapshots not referenced by a project head.',
    ],
    accent: COLORS.accent,
  },
  {
    title: 'Relationship resolution',
    summary: 'Resolved targets are convenient, while the locator remains the durable truth.',
    items: [
      'edge_key is a non-null deterministic key, avoiding cross-dialect NULL uniqueness differences.',
      'to_project_key, to_root_key, and to_identity_key disambiguate repeated symbols across roots.',
      'to_node_id is set only for a target in the same snapshot; external targets remain unresolved.',
      'Deleting a target sets to_node_id null but preserves the locator for later re-resolution.',
    ],
    accent: COLORS.fk,
  },
  {
    title: 'Lossless payload and order',
    summary: 'Relational projections must reconstruct the canonical UIR JSON without duplicating whole subtrees.',
    items: [
      'document_payload stores UIR root data outside node collections, including Hierarchy and RawFiles.',
      'Each node payload stores node-local JSON; traversed child collections are represented by parent_id, child_slot, and ordinal.',
      'Embedded non-tree values, statements, endpoint input/output records, and SourceCode remain in the local payload.',
      'payload_schema versions the split rules; canonical rehydration must byte-normalize equal before ready.',
    ],
    accent: COLORS.outputBorder,
  },
  {
    title: 'Physical scope constraints',
    summary: 'Deliberate scope columns let the database reject cross-boundary references.',
    items: [
      'Composite keys keep parent nodes and node locations inside one root.',
      'Relationship sources share from_root_id; resolved endpoints share snapshot_id.',
      'Redundant scope IDs are validated projections, not independent mutable ownership fields.',
      'Ingestion must capture locations before Coalesce discards secondary SourceCode values.',
    ],
    accent: COLORS.fk,
  },
];

const gormCards: DetailCardDef[] = [
  {
    title: 'Runtime properties',
    summary: 'One DSN selects the migration target and matching GORM dialector.',
    items: [
      'sqlite:// URLs and plain .db paths select SQLite; PostgreSQL URLs and keyword DSNs select PostgreSQL.',
      'UirDB routes the same DSN through commons-db migration and connection seams; unsupported schemes fail startup.',
      'SQLite is file-backed and enables foreign_keys, a bounded busy_timeout, WAL, and a single-connection pool.',
      'PostgreSQL schema selection remains a runtime property, not an identity field.',
    ],
  },
  {
    title: 'Logical type mapping',
    summary: 'The Go model stays stable while the migration chooses native database types.',
    items: [
      'ID maps to PostgreSQL UUID and canonical lowercase UUID text in SQLite.',
      'JSON maps to PostgreSQL JSONB and validated JSON text in SQLite.',
      'Timestamp is UTC time.Time at microsecond precision: TIMESTAMPTZ in PostgreSQL and DATETIME in SQLite.',
      'Bool maps to PostgreSQL BOOLEAN and SQLite BOOL; no integer sentinel leaks into the Go API.',
    ],
    accent: COLORS.outputBorder,
  },
  {
    title: 'Migrations and constraints',
    summary: 'GORM is the persistence API, not permission to delegate schema correctness to AutoMigrate.',
    items: [
      'One PostgreSQL-shaped HCL source declares tables, indexes, foreign-key actions, and creation order.',
      'commons-db evaluates it directly for PostgreSQL and projects the portable subset for SQLite.',
      'Unsupported SQLite objects fail initialization; both targets are migration-tested from the same source.',
      'Do not add gorm.DeletedAt: immutable snapshot retention is explicit and cascade-driven.',
    ],
    accent: COLORS.fk,
  },
  {
    title: 'Write and query behavior',
    summary: 'All user-visible reads begin at the published project head.',
    items: [
      'Use bounded batch transactions while state is building; never upsert into a ready or active snapshot.',
      'Use the declared composite unique keys for conflict handling, never a dialect-specific row id.',
      'Index graph traversal by from_node_id, to_node_id, relationship_type, and snapshot_id.',
      'Keep identity and common filters in columns; payload JSON is lossless data, not the primary query plan.',
    ],
    accent: COLORS.accent,
  },
];

const adversarialFindings: ReviewFinding[] = [
  {
    scenario: 'Two roots expose the same module/package/type',
    failure: 'Identifier.SymbolKey() produces the same value and a global unique key merges unrelated symbols.',
    control: 'Nodes are unique by (root_id, identity_key); relationship locators include project and root keys.',
  },
  {
    scenario: 'Identifier components contain separator characters',
    failure: 'The dotted SymbolKey string is not injective and can collapse different structured identifiers.',
    control: 'identity_key hashes canonical length-delimited fields and explicit ID when present; symbol_key is not unique.',
  },
  {
    scenario: 'One repository is checked out as two worktrees',
    failure: 'Absolute paths or remote URI alone collapse two mounted views or make identity host-specific.',
    control: 'Distinct root_key and mount_path identify each mount; local_path remains non-identity diagnostics.',
  },
  {
    scenario: 'A worktree is dirty at an unchanged commit',
    failure: 'Revision-only snapshot identity reuses stale analysis for bytes that are not in the commit.',
    control: 'Each root records content_set_hash and the snapshot revision-set hash includes it.',
  },
  {
    scenario: 'A submodule pins an independent commit',
    failure: 'Treating it as a parent directory attributes files to the wrong revision and invalidates cache keys.',
    control: 'The submodule is a child root with its own repository_key, revision, and root-scoped sources.',
  },
  {
    scenario: 'A nested Git repository is not declared as a submodule',
    failure: 'A parent-root scan double-ingests files or silently assigns them to the outer repository.',
    control: 'Discovery records every Git boundary; deepest-root ownership assigns each source exactly once.',
  },
  {
    scenario: 'A logical type is assembled from several files',
    failure: 'A node.source_id foreign key discards provenance or duplicates one canonical symbol per file.',
    control: 'Nodes belong to roots; uir_node_locations retains every source span and the primary location.',
  },
  {
    scenario: 'Coalesce runs before persistence',
    failure: 'Current UIR Coalesce keeps one representative SourceCode location and discards the others.',
    control: 'Capture node-location rows before Coalesce or ingest an explicit provenance set; never infer lost locations.',
  },
  {
    scenario: 'A flattened node tree omits child slot or order',
    failure: 'Arrays such as methods, columns, indexes, and init functions cannot be reconstructed losslessly.',
    control: 'Store child_slot and ordinal with a versioned local-payload split and verify canonical rehydration.',
  },
  {
    scenario: 'UIR root data is reduced to node rows',
    failure: 'Hierarchy and RawFiles disappear because neither is a Node child collection.',
    control: 'Snapshot document_payload preserves all non-node root fields under the versioned payload schema.',
  },
  {
    scenario: 'A scan crashes after deleting old rows',
    failure: 'Readers observe a half-built graph or no graph at all.',
    control: 'Snapshots are immutable and invisible while building; one project-head swap publishes atomically.',
  },
  {
    scenario: 'Two publishers race for the same project',
    failure: 'Last-writer-wins can activate a stale scan without detection.',
    control: 'uir_project_heads.version is updated with compare-and-swap; a conflict aborts publication.',
  },
  {
    scenario: 'A relationship target is outside the loaded roots',
    failure: 'A required to_node_id either drops the edge or fabricates a local node.',
    control: 'The scoped target locator is mandatory; to_node_id stays nullable until same-snapshot resolution.',
  },
  {
    scenario: 'Nullable fields participate in a wide unique edge index',
    failure: 'SQLite and PostgreSQL NULL semantics allow duplicates that appear equivalent to the application.',
    control: 'A required edge_key hashes the normalized edge identity and is unique with from_node_id.',
  },
  {
    scenario: 'A path contains dot-dot, symlink escape, or credentials',
    failure: 'Rows escape root ownership, collide across hosts, or persist secrets.',
    control: 'Normalize before persistence, reject traversal, require an explicit external root, and redact URIs.',
  },
  {
    scenario: 'The same checkout is scanned on hosts with different case rules',
    failure: 'Host-derived normalization changes path keys and creates false adds, deletes, or collisions.',
    control: 'Each root persists path_case and normalization_version; path keys follow that contract on every host.',
  },
  {
    scenario: 'A target node is deleted during retention',
    failure: 'Cascading the target edge destroys evidence about an unresolved dependency.',
    control: 'ON DELETE SET NULL removes only to_node_id; the target locator remains resolvable.',
  },
  {
    scenario: 'SQLite and PostgreSQL migrations drift',
    failure: 'A design that works under one dialector silently loses constraints or native JSON behavior.',
    control: 'One canonical HCL bundle is projected by commons-db; integration tests assert indexes, FKs, cascades, and JSON on both.',
  },
];

function FieldRow({ name, type, markers = [] }: FieldDef) {
  return (
    <div className="flex items-center gap-2 py-0.5">
      <div className="flex w-[15mm] shrink-0 gap-0.5">
        {markers.map((marker) => (
          <span
            key={marker}
            className="rounded px-1 text-[6pt] font-bold text-white"
            style={{ backgroundColor: markerColors[marker] }}
          >
            {marker}
          </span>
        ))}
      </div>
      <span className="text-[8pt] font-semibold" style={{ color: COLORS.accent }}>
        {name}
      </span>
      <span className="ml-auto text-[7pt]" style={{ color: COLORS.muted }}>
        {type}
      </span>
    </div>
  );
}

function EntityBox({
  id,
  title,
  description,
  fields,
  constraints,
  accent = COLORS.primary,
  minWidth = '66mm',
}: EntityBoxProps) {
  return (
    <BoxNode
      id={id}
      title={title}
      headerColor={accent}
      bodyColor={COLORS.background}
      borderColor={accent}
      compact
      minWidth={minWidth}
    >
      <div className="mb-2 text-[7pt] leading-snug" style={{ color: COLORS.muted }}>
        {description}
      </div>
      <div className="flex flex-col">
        {fields.map((field) => (
          <FieldRow key={field.name} {...field} />
        ))}
      </div>
      <div className="mt-2 border-t pt-1.5" style={{ borderColor: COLORS.muted }}>
        {constraints.map((constraint) => (
          <div key={constraint} className="text-[6pt] leading-snug" style={{ color: COLORS.muted }}>
            {constraint}
          </div>
        ))}
      </div>
    </BoxNode>
  );
}

function RelLabel({ text }: { text: string }) {
  return (
    <div
      className="whitespace-nowrap rounded px-1.5 py-0.5 text-[6pt] font-semibold"
      style={{
        backgroundColor: COLORS.background,
        border: `1px solid ${COLORS.muted}`,
        color: COLORS.muted,
      }}
    >
      {text}
    </div>
  );
}

function DetailCard({ title, summary, items, accent = COLORS.primary }: DetailCardDef) {
  return (
    <div className="rounded-xl border p-4" style={{ backgroundColor: COLORS.background, borderColor: accent }}>
      <h4 className="m-0 text-[11pt] font-semibold" style={{ color: accent }}>
        {title}
      </h4>
      <p className="mb-2 mt-1 text-[8pt] leading-relaxed" style={{ color: COLORS.muted }}>
        {summary}
      </p>
      <ul className="m-0 space-y-1 pl-5 text-[8pt] leading-relaxed" style={{ color: COLORS.text }}>
        {items.map((item) => (
          <li key={item}>{item}</li>
        ))}
      </ul>
    </div>
  );
}

function DiagramHeading({ title, subtitle }: { title: string; subtitle: string }) {
  return (
    <div className="mb-[6mm]">
      <h3 className="m-0 mb-[2mm] text-[14pt] font-semibold leading-[18pt]" style={{ color: COLORS.accent }}>
        {title}
      </h3>
      <p className="m-0 text-[9pt] leading-[13pt]" style={{ color: COLORS.muted }}>
        {subtitle}
      </p>
    </div>
  );
}

function ScopeArrows({ id }: { id: IdFactory }) {
  return (
    <>
      <Arrow variant="er" from={id('snapshots')} to={id('projects')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <RelLabel text="N:1 project" /> }} />
      <Arrow variant="er" from={id('roots')} to={id('snapshots')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <RelLabel text="N:1 snapshot" /> }} />
      <Arrow variant="er" from={id('sources')} to={id('roots')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <RelLabel text="N:1 root" /> }} />
      <Arrow variant="er" color={COLORS.outputBorder} from={id('heads')} to={id('projects')} path="straight" startAnchor={{ position: 'top', offset: { x: -45 } }} endAnchor="bottom" labels={{ middle: <RelLabel text="1:1 project" /> }} />
      <Arrow variant="er" color={COLORS.outputBorder} from={id('heads')} to={id('snapshots')} path="straight" startAnchor={{ position: 'top', offset: { x: 45 } }} endAnchor="bottom" labels={{ middle: <RelLabel text="1:1 active" /> }} />
    </>
  );
}

function ScopeDiagram() {
  return (
    <Diagram className="relative mx-auto w-[300mm] py-6">
      {(id) => (
        <>
          <DiagramHeading
            title="UIR GORM Persistence Model · Scope and provenance"
            subtitle="Projects publish immutable snapshots. Every source is owned by one explicit root, including nested repositories and submodules."
          />
          <div className="flex items-center justify-center gap-10">
            <EntityBox id={id('projects')} {...projectEntity} />
            <EntityBox id={id('snapshots')} {...snapshotEntity} />
            <EntityBox id={id('roots')} {...rootEntity} />
            <EntityBox id={id('sources')} {...sourceEntity} />
          </div>
          <div className="mt-16 flex justify-center">
            <EntityBox id={id('heads')} {...projectHeadEntity} />
          </div>
          <ScopeArrows id={id} />
        </>
      )}
    </Diagram>
  );
}

function GraphArrows({ id }: { id: IdFactory }) {
  return (
    <>
      <Arrow variant="er" color={COLORS.outputBorder} from={id('locations')} to={id('sources')} path="straight" startAnchor="left" endAnchor="right" labels={{ middle: <RelLabel text="N:1 source" /> }} />
      <Arrow variant="er" color={COLORS.outputBorder} from={id('locations')} to={id('nodes')} path="straight" startAnchor="right" endAnchor="left" labels={{ middle: <RelLabel text="N:1 node" /> }} />
      <Arrow variant="er" from={id('relationships')} to={id('nodes')} path="straight" startAnchor={{ position: 'left', offset: { y: -55 } }} endAnchor={{ position: 'right', offset: { y: -55 } }} labels={{ middle: <RelLabel text="from N:1" /> }} />
      <Arrow variant="er" from={id('relationships')} to={id('nodes')} path="straight" startAnchor={{ position: 'left', offset: { y: 55 } }} endAnchor={{ position: 'right', offset: { y: 55 } }} labels={{ middle: <RelLabel text="to N:0..1" /> }} />
      <Arrow variant="er" color={COLORS.fk} from={id('fields')} to={id('nodes')} path="straight" startAnchor="top" endAnchor="bottom" labels={{ middle: <RelLabel text="0..1:1 detail" /> }} />
    </>
  );
}

function GraphDiagram() {
  return (
    <Diagram className="relative mx-auto w-[300mm] py-6">
      {(id) => (
        <>
          <DiagramHeading
            title="Semantic graph and source locations"
            subtitle="Root-scoped symbols remain canonical while source spans, structured field detail, and graph edges stay independently queryable."
          />
          <div className="flex items-center justify-center gap-10">
            <EntityBox
              id={id('sources')}
              title="uir_sources · reference"
              description="The source identity defined on the provenance page."
              minWidth="54mm"
              fields={[
                { name: 'id', type: 'ID', markers: ['PK'] },
                { name: 'root_id', type: 'ID', markers: ['FK', 'IX'] },
                { name: 'path_key', type: 'String', markers: ['IX'] },
              ]}
              constraints={['UQ (root_id, path_key)']}
            />
            <EntityBox id={id('locations')} {...nodeLocationEntity} />
            <EntityBox id={id('nodes')} {...nodeEntity} />
            <EntityBox id={id('relationships')} {...relationshipEntity} />
          </div>
          <div className="mt-16 flex justify-center">
            <EntityBox id={id('fields')} {...fieldEntity} />
          </div>
          <GraphArrows id={id} />
        </>
      )}
    </Diagram>
  );
}

function DialectLegend() {
  const mappings = [
    ['ID', 'PostgreSQL UUID', 'SQLite TEXT'],
    ['JSON', 'PostgreSQL JSONB', 'SQLite validated TEXT'],
    ['Timestamp', 'PostgreSQL TIMESTAMPTZ', 'SQLite DATETIME'],
    ['Bool', 'PostgreSQL BOOLEAN', 'SQLite BOOL'],
  ];

  return (
    <div className="mt-6 flex items-center justify-center gap-3">
      <span className="text-[7pt] font-bold uppercase tracking-wide" style={{ color: COLORS.muted }}>
        Dialect mapping
      </span>
      {mappings.map(([logical, postgres, sqlite]) => (
        <div
          key={logical}
          className="rounded-lg border px-3 py-1.5 text-[7pt]"
          style={{ backgroundColor: COLORS.background, borderColor: COLORS.primary, color: COLORS.muted }}
        >
          <span className="font-bold" style={{ color: COLORS.accent }}>
            {logical}
          </span>
          {' · '}
          {postgres}
          {' · '}
          {sqlite}
        </div>
      ))}
    </div>
  );
}

function DocumentationPage() {
  return (
    <Page pageSize="a3-landscape" margins={{ top: 8, right: 8, bottom: 8, left: 8 }}>
      <Section title="Storage design contract">
        <p className="mb-5 mt-0 text-[9pt] leading-relaxed" style={{ color: COLORS.muted }}>
          These rules are part of the schema contract. Implementations should reject invalid scope or path state instead of repairing it with implicit fallbacks.
        </p>
        <div className="grid grid-cols-2 gap-4">
          {storageCards.map((card) => (
            <DetailCard key={card.title} {...card} />
          ))}
        </div>
      </Section>
      <Section title="GORM and database portability">
        <div className="grid grid-cols-2 gap-4">
          {gormCards.map((card) => (
            <DetailCard key={card.title} {...card} />
          ))}
        </div>
        <DialectLegend />
      </Section>
    </Page>
  );
}

function AdversarialReviewPage() {
  return (
    <Page pageSize="a3-landscape" margins={{ top: 8, right: 8, bottom: 8, left: 8 }}>
      <Section title="Adversarial review">
        <div className="mb-4 rounded-xl border p-4" style={{ borderColor: COLORS.fk, backgroundColor: COLORS.background }}>
          <h4 className="m-0 text-[11pt] font-semibold" style={{ color: COLORS.fk }}>
            Result: the original four-table model was rejected
          </h4>
          <p className="mb-0 mt-1 text-[8pt] leading-relaxed" style={{ color: COLORS.muted }}>
            Direct source ownership and globally unique symbol keys fail for coalesced nodes, multiple roots, worktrees, and submodules. The revised design adds snapshot, root, publication-head, and location boundaries so those cases are explicit and enforceable.
          </p>
        </div>
        <div className="overflow-hidden rounded-xl border" style={{ borderColor: COLORS.primary }}>
          <div className="grid grid-cols-[1fr_1.25fr_1.45fr] text-[8pt] font-bold" style={{ backgroundColor: COLORS.primary, color: COLORS.background }}>
            <div className="p-2">Scenario</div>
            <div className="p-2">Failure in a naive model</div>
            <div className="p-2">Required control</div>
          </div>
          {adversarialFindings.map((finding, index) => (
            <div
              key={finding.scenario}
              className="grid grid-cols-[1fr_1.25fr_1.45fr] border-t text-[7pt] leading-relaxed"
              style={{
                backgroundColor: index % 2 === 0 ? COLORS.background : COLORS.surface,
                borderColor: COLORS.muted,
                color: COLORS.text,
              }}
            >
              <div className="p-2 font-semibold" style={{ color: COLORS.accent }}>
                {finding.scenario}
              </div>
              <div className="p-2">{finding.failure}</div>
              <div className="p-2">{finding.control}</div>
            </div>
          ))}
        </div>
      </Section>
      <Section title="Acceptance invariants">
        <div className="grid grid-cols-3 gap-4">
          <DetailCard
            title="Before ready"
            summary="Validation must complete before a snapshot can be published."
            items={[
              'Every source resolves to one root.',
              'Every node resolves to one root in its snapshot.',
              'Every node location keeps node and source in the same root.',
              'Every edge source shares the from-node root; every resolved target shares the snapshot.',
              'Canonical rehydration preserves node order, Hierarchy, RawFiles, and local payloads.',
            ]}
            accent={COLORS.fk}
          />
          <DetailCard
            title="At publication"
            summary="The active graph changes exactly once."
            items={[
              'Snapshot state changes from building to ready.',
              'Head update checks the previous version.',
              'Head project_id matches snapshot project_id.',
              'Readers select through uir_project_heads only.',
            ]}
            accent={COLORS.accent}
          />
          <DetailCard
            title="During retention"
            summary="History cleanup cannot damage the published graph."
            items={[
              'Never delete a snapshot referenced by a project head.',
              'Cascade only within the selected historical snapshot.',
              'Preserve unresolved edge locators when targets disappear.',
              'Record counts and hashes are auditable before deletion.',
            ]}
            accent={COLORS.outputBorder}
          />
        </div>
      </Section>
    </Page>
  );
}

export default function UIRGormStorageDesign() {
  return (
    <>
      <Page pageSize="a3-landscape" margins={{ top: 8, right: 8, bottom: 8, left: 8 }}>
        <Section>
          <ScopeDiagram />
        </Section>
      </Page>
      <Page pageSize="a3-landscape" margins={{ top: 8, right: 8, bottom: 8, left: 8 }}>
        <Section>
          <GraphDiagram />
        </Section>
      </Page>
      <DocumentationPage />
      <AdversarialReviewPage />
    </>
  );
}

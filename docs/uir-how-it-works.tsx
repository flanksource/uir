// @live
import React from 'react';
import Arrow from '@flanksource/facet/diagram/Arrow';
import BoxNode from '@flanksource/facet/diagram/BoxNode';
import { COLORS } from '@flanksource/facet/diagram/colors';
import { NodePill, NodeSection, SectionDivider } from '@flanksource/facet/diagram/content';
import Diagram from '@flanksource/facet/diagram/Diagram';
import Page from '@flanksource/facet/Page';
import Section from '@flanksource/facet/Section';
import { Go, Java, K8S, K8SConfigmap, K8SPod, K8SService, Postgres, Python, SqlServer, Ts, Xml } from '@flanksource/icons/mi';

type IconComponent = React.ComponentType<{ className?: string }>;

const mono = 'font-mono text-[8.5pt] leading-[1.45]';

function UirMark({ className }: { className?: string }) {
  return <svg viewBox="0 0 256 256" className={className} aria-hidden="true">
    <path fill="currentColor" d="M42 36H56V140A72 72 0 0 0 81.72 195.15A3 3 0 0 0 85.58 190.56A66 66 0 0 1 62 140V36H76V140A52 52 0 0 0 94.57 179.83A3 3 0 0 0 98.43 175.24A46 46 0 0 1 82 140V36H96V140A32 32 0 0 0 160 140V36H174V140A46 46 0 0 1 157.57 175.24A3 3 0 0 0 161.43 179.83A52 52 0 0 0 180 140V36H194V140A66 66 0 0 1 170.42 190.56A3 3 0 0 0 174.28 195.15A72 72 0 0 0 200 140V36H214V140A86 86 0 0 1 42 140Z" />
  </svg>;
}

function Command({ children, color }: { children: React.ReactNode; color: string }) {
  return <code className="whitespace-nowrap rounded px-1.5 py-0.5 font-mono text-[8.5pt]"
    style={{ backgroundColor: color, color: 'white' }}>{children}</code>;
}

function Panel({ children, className = '' }: { children: React.ReactNode; className?: string }) {
  return <div className={`rounded-md border bg-white px-2.5 py-2 ${className}`} style={{ borderColor: COLORS.primary }}>{children}</div>;
}

function Caption({ children }: { children: React.ReactNode }) {
  return <p className="mt-2 text-[9pt] leading-snug" style={{ color: COLORS.muted }}>{children}</p>;
}

function Block({ id, title, command, color = COLORS.primary, width = '250px', dashed, children }: {
  id: string; title: string; command?: string; color?: string; width?: string; dashed?: boolean; children: React.ReactNode;
}) {
  return <BoxNode id={id} title={<span className="text-sm">{title}</span>} headerColor={color} borderColor={color}
    bodyColor={COLORS.background} compact minWidth={width} dashed={dashed}>
    <div style={{ width: `calc(${width} - 24px)` }}>
      {command && <div className="mb-2 flex justify-end"><Command color={color}>{command}</Command></div>}
      {children}
    </div>
  </BoxNode>;
}

function Lowerer({ parser, note, icons }: { parser: string; note: string; icons: IconComponent[] }) {
  return <div className="flex items-center gap-2 border-b py-1.5 last:border-b-0" style={{ borderColor: COLORS.background }}>
    <div className="flex flex-col">
      <span className={`font-bold ${mono}`} style={{ color: COLORS.accent }}>{parser}</span>
      <span className="text-[8.5pt]" style={{ color: COLORS.muted }}>{note}</span>
    </div>
    <div className="ml-auto flex gap-1.5">{icons.map((Icon, index) => <Icon key={index} className="h-5 w-5" />)}</div>
  </div>;
}

/*
 * The canonical notation for the model: a symbol is a node whose outline is its kind's category and
 * whose caption is the kind; a relationship is an arrow, unlabelled for `calls`, labelled with its kind
 * otherwise, and dashed with its guard when it only happens under a condition.
 */
type KindCategory = 'container' | 'typed' | 'callable' | 'member';

const BUILTIN_KIND_CATEGORIES: Readonly<Record<string, KindCategory>> = {
  package: 'container', module: 'container',
  type: 'typed', interface: 'typed', record: 'typed', table: 'typed',
  func: 'callable', method: 'callable', constructor: 'callable', endpoint: 'callable',
  field: 'member', var: 'member', const: 'member', builtin: 'member', column: 'member', index: 'member',
  foreign_key: 'member', annotation: 'member',
};

const CATEGORY_OUTLINES: Readonly<Record<KindCategory, { rx: number; dash?: string }>> = {
  callable: { rx: 5 },
  typed: { rx: 1 },
  member: { rx: 13 },
  container: { rx: 3, dash: '4 2' },
};

type GraphSymbol = {
  key: string; x: number; y: number; name: string; kind: string; category?: KindCategory; focus?: boolean; Icon?: IconComponent;
};

const SYMBOL_ICON_SIZE = 16;
type GraphRelationship = { from: string; to: string; kind: string; guard?: string };

const SYMBOL_HEIGHT = 28;

function kindCategory(symbol: GraphSymbol): KindCategory {
  const category = symbol.category ?? BUILTIN_KIND_CATEGORIES[symbol.kind];
  if (!category) throw new Error(`symbol ${symbol.key} has custom kind ${symbol.kind} but no category`);
  return category;
}

function SymbolGraph({ width, height, symbolWidth, symbols, relationships }: {
  width: number; height: number; symbolWidth: number; symbols: GraphSymbol[]; relationships: GraphRelationship[];
}) {
  const marker = `uir-relationship-${React.useId().replace(/[^a-zA-Z0-9]/g, '')}`;
  const lookup = (key: string) => {
    const symbol = symbols.find((candidate) => candidate.key === key);
    if (!symbol) throw new Error(`relationship references unknown symbol ${key}`);
    return symbol;
  };
  const centre = (symbol: GraphSymbol) => ({ x: symbol.x + symbolWidth / 2, y: symbol.y + SYMBOL_HEIGHT / 2 });
  const outlinePoint = (symbol: GraphSymbol, toward: { x: number; y: number }) => {
    const from = centre(symbol);
    const dx = toward.x - from.x;
    const dy = toward.y - from.y;
    const scale = Math.min(Math.abs(symbolWidth / 2 / dx), Math.abs(SYMBOL_HEIGHT / 2 / dy));
    return { x: from.x + dx * scale, y: from.y + dy * scale };
  };
  const text = { fontFamily: 'ui-monospace, monospace' };
  return <svg viewBox={`0 0 ${width} ${height}`} className="w-full">
    <defs>
      <marker id={marker} viewBox="0 0 6 6" refX="5" refY="3" markerWidth="5" markerHeight="5" orient="auto">
        <path d="M0 0L6 3L0 6Z" fill={COLORS.muted} />
      </marker>
    </defs>
    {relationships.map((relationship) => {
      const from = lookup(relationship.from);
      const to = lookup(relationship.to);
      const start = outlinePoint(from, centre(to));
      const end = outlinePoint(to, centre(from));
      const label = relationship.guard ?? (relationship.kind === 'calls' ? undefined : relationship.kind);
      return <g key={`${relationship.from}-${relationship.to}`}>
        <line x1={start.x} y1={start.y} x2={end.x} y2={end.y} stroke={COLORS.muted} strokeWidth="1.2"
          strokeDasharray={relationship.guard ? '3 2' : undefined} markerEnd={`url(#${marker})`} />
        {label && <text x={(start.x + end.x) / 2} y={(start.y + end.y) / 2 + 2.5} textAnchor="middle" fontSize="7.5"
          fill={COLORS.muted} stroke="white" strokeWidth="3" paintOrder="stroke" {...text}>{label}</text>}
      </g>;
    })}
    {symbols.map((symbol) => {
      const outline = CATEGORY_OUTLINES[kindCategory(symbol)];
      const { Icon } = symbol;
      const iconLeft = symbol.x + outline.rx / 2 + 5;
      const textCentre = Icon ? (iconLeft + SYMBOL_ICON_SIZE + symbol.x + symbolWidth) / 2 : symbol.x + symbolWidth / 2;
      return <g key={symbol.key}>
        <rect x={symbol.x} y={symbol.y} width={symbolWidth} height={SYMBOL_HEIGHT} rx={outline.rx}
          fill={symbol.focus ? COLORS.primary : 'white'} stroke={COLORS.primary} strokeDasharray={outline.dash} />
        {Icon && <foreignObject x={iconLeft} y={symbol.y + (SYMBOL_HEIGHT - SYMBOL_ICON_SIZE) / 2}
          width={SYMBOL_ICON_SIZE} height={SYMBOL_ICON_SIZE}>
          <div style={{ fontSize: `${SYMBOL_ICON_SIZE}px`, lineHeight: 0 }}><Icon /></div>
        </foreignObject>}
        <text x={textCentre} y={symbol.y + 10.5} textAnchor="middle" fontSize="7.5"
          fill={symbol.focus ? 'white' : COLORS.muted} opacity={symbol.focus ? 0.85 : 1} {...text}>{symbol.kind}</text>
        <text x={textCentre} y={symbol.y + 22} textAnchor="middle" fontSize="9"
          fill={symbol.focus ? 'white' : COLORS.accent} {...text}>{symbol.name}</text>
      </g>;
    })}
  </svg>;
}

function Glyph({ children }: { children: React.ReactNode }) {
  return <svg viewBox="0 0 14 14" width="13" height="13" className="shrink-0" fill="none" stroke={COLORS.muted} strokeWidth="1.2"
    strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{children}</svg>;
}

const MODEL_GLYPHS: Readonly<Record<string, React.ReactNode>> = {
  Symbols: <Glyph><rect x="1.5" y="3.5" width="11" height="7" rx="2" /></Glyph>,
  Relationships: <Glyph><circle cx="3" cy="7" r="2.2" fill={COLORS.muted} stroke="none" /><path d="M5.5 7H12.5M9.5 4L12.5 7L9.5 10" /></Glyph>,
  Kinds: <Glyph><path d="M1.5 2.5H7.5L12.5 7L7.5 11.5H1.5Z" /><circle cx="4.5" cy="7" r="1.1" fill={COLORS.muted} stroke="none" /></Glyph>,
  Metrics: <Glyph><path d="M2.5 12.5V8.5M7 12.5V3M11.5 12.5V6" /></Glyph>,
  Documents: <Glyph><path d="M3 1.5H8.5L11.5 4.5V12.5H3Z M8.5 1.5V4.5H11.5" /></Glyph>,
};

function ModelPill({ item }: { item: string }) {
  const glyph = MODEL_GLYPHS[item];
  if (!glyph) throw new Error(`no glyph for model element ${item}`);
  return <NodePill><span className="inline-flex items-center gap-1.5">{glyph}{item}</span></NodePill>;
}

function UirHub({ id, compact = false }: { id: string; compact?: boolean }) {
  return <div id={id} className="overflow-hidden border-2 shadow-2xl"
    style={{ borderColor: COLORS.primary, backgroundColor: COLORS.background, borderRadius: '16px', minWidth: compact ? '440px' : '290px' }}>
    <div className="flex items-center justify-center gap-2 px-6 py-3" style={{ backgroundColor: COLORS.primary }}>
      <UirMark className="h-7 w-7 text-white" />
      <span className="text-lg font-bold tracking-wide text-white">UIR</span>
    </div>
    {compact
      ? <div className="flex justify-center gap-2 p-3">
        {['Symbols', 'Relationships', 'Kinds', 'Metrics', 'Documents'].map((item) => <ModelPill key={item} item={item} />)}
      </div>
      : <div className="flex flex-col gap-3 p-4">
        <NodeSection title="Model · Go, Python, Java" items={['Nodes and statements', 'Symbols and relationships', 'Semantic hashes']} />
        <SectionDivider />
        <NodeSection title="Snapshot store" items={['Immutable module snapshots', 'Symbol index', 'SQLite or PostgreSQL']} />
      </div>}
  </div>;
}

export function IntoUIR() {
  return <Diagram className="relative">
    {(id) => <>
      <div className="grid px-6 py-8"
        style={{ gridTemplateColumns: 'auto auto auto', gridTemplateRows: 'auto auto auto', columnGap: '96px', rowGap: '24px',
          justifyItems: 'start', alignItems: 'center', justifyContent: 'center' }}>
        <div style={{ gridColumn: 1, gridRow: 1 }}>
          <Block id={id('lang')} title="Lowering" command="uir add" width="280px">
            <Lowerer parser="go/ast + go/types" note="Native, fully typed" icons={[Go]} />
            <Lowerer parser="tree-sitter" note="arch-unit plugin" icons={[Python, Java, Ts]} />
            <Lowerer parser="sqlglot-go" note="PL/pgSQL and T-SQL routines" icons={[Postgres, SqlServer]} />
            <Lowerer parser="encoding/xml" note="Custom, via lower.Recorder" icons={[Xml]} />
          </Block>
        </div>
        <div style={{ gridColumn: 1, gridRow: 2 }}>
          <Block id={id('k8s')} title="Kubernetes domain" width="280px">
            <div className="mb-2 flex items-center gap-2">
              <K8S className="h-6 w-6" />
              <span className="text-[9pt]" style={{ color: COLORS.muted }}>Services, pods and their links</span>
            </div>
            <Panel>
              <SymbolGraph width={268} height={84} symbolWidth={106} symbols={[
                { key: 'service', x: 0, y: 28, name: 'checkout', kind: 'k8s.service', category: 'member', Icon: K8SService },
                { key: 'pod', x: 162, y: 0, name: 'checkout-7d9', kind: 'k8s.pod', category: 'member', Icon: K8SPod },
                { key: 'config', x: 162, y: 56, name: 'checkout-env', kind: 'k8s.configmap', category: 'member', Icon: K8SConfigmap },
              ]} relationships={[
                { from: 'service', to: 'pod', kind: 'selects' },
                { from: 'service', to: 'config', kind: 'uses_config' },
              ]} />
            </Panel>
          </Block>
        </div>
        <div style={{ gridColumn: 1, gridRow: 3 }}>
          <Block id={id('oipa')} title="Bring your own domain" width="280px" dashed>
            <div className="mb-2 text-[9pt]" style={{ color: COLORS.muted }}>For example OIPA, whose XML configuration oipa-cli publishes</div>
            <div className="grid grid-cols-2 gap-1">
              {['oipa.transaction', 'oipa.br', 'oipa.segment', 'oipa.screen'].map((kind) =>
                <span key={kind} className={`rounded border bg-white px-1.5 py-0.5 text-center ${mono}`}
                  style={{ borderColor: COLORS.primary, color: COLORS.accent }}>{kind}</span>)}
            </div>
          </Block>
        </div>
        <div style={{ gridColumn: 2, gridRow: '2 / span 2' }}>
          <Block id={id('json')} title="JSON publication" command="uir import" width="300px">
            <Panel>
              <pre className={mono} style={{ color: COLORS.accent }}>{`{
  "root_key": "example.org/checkout",
  "kinds": [{
    "name": "k8s.service",
    "category": "member"
  }],
  "symbols": [{
    "kind": "k8s.service",
    "name": "checkout"
  }],
  "documents": [ … ]
}`}</pre>
            </Panel>
            <Caption>Custom kinds carry JSON Schema-typed props. Validated before the head moves.</Caption>
          </Block>
        </div>
        <div style={{ gridColumn: 3, gridRow: '1 / span 3', alignSelf: 'start' }}>
          <UirHub id={id('uir')} />
        </div>
      </div>
      <Arrow variant="primary" from={id('lang')} to={id('uir')} path="straight"
        startAnchor={{ position: 'right', offset: { y: -50 } }}
        endAnchor={{ position: 'left', offset: { y: -110 } }} labels={{ middle: <NodePill>Go: direct</NodePill> }} />
      <Arrow variant="secondary" from={id('lang')} to={id('json')} path="straight"
        startAnchor={{ position: 'right', offset: { y: 40 } }}
        endAnchor={{ position: 'left', offset: { y: -120 } }} />
      <Arrow variant="primary" from={id('k8s')} to={id('json')} path="straight" startAnchor="right"
        endAnchor={{ position: 'left', offset: { y: -50 } }} />
      <Arrow variant="primary" from={id('oipa')} to={id('json')} path="straight" startAnchor="right"
        endAnchor={{ position: 'left', offset: { y: 50 } }} />
      <Arrow variant="primary" from={id('json')} to={id('uir')} path="straight" startAnchor="right"
        endAnchor={{ position: 'left', offset: { y: 60 } }} />
    </>}
  </Diagram>;
}

function CallGraph() {
  return <SymbolGraph width={164} height={140} symbolWidth={76} symbols={[
    { key: 'post', x: 0, y: 0, name: 'Handler.Post', kind: 'method' },
    { key: 'run', x: 88, y: 0, name: 'Job.Run', kind: 'method' },
    { key: 'save', x: 44, y: 56, name: 'Store.Save', kind: 'method', focus: true },
    { key: 'warn', x: 0, y: 112, name: 'log.Warn', kind: 'func' },
    { key: 'exec', x: 88, y: 112, name: 'db.Exec', kind: 'method' },
  ]} relationships={[
    { from: 'post', to: 'save', kind: 'calls' },
    { from: 'run', to: 'save', kind: 'calls' },
    { from: 'save', to: 'warn', kind: 'calls', guard: 'if err' },
    { from: 'save', to: 'exec', kind: 'calls' },
  ]} />;
}

function SymbolDiff() {
  const row = (symbolClass: string, symbol: string, detail: string, color: string) =>
    <div key={symbol} className={`grid grid-cols-[72px_1fr] ${mono}`}>
      <span className="font-bold" style={{ color }}>{symbolClass}</span>
      <span style={{ color: COLORS.accent }}>{symbol} <span style={{ color: COLORS.muted }}>{detail}</span></span>
    </div>;
  return <Panel>
    {row('signature', 'Save', '+opts', COLORS.accent)}
    {row('body', 'Retry', '', COLORS.accent)}
    {row('moved', 'Ledger.post', '', COLORS.accent)}
    {row('added', 'Backoff', '', COLORS.outputBorder)}
    {row('removed', 'LegacySave', '', COLORS.muted)}
    <div className={`mt-1.5 border-t pt-1 ${mono}`} style={{ borderColor: COLORS.primary, color: COLORS.muted }}>
      1 API change
    </div>
  </Panel>;
}

function LintFinding() {
  return <Panel>
    <div className={mono} style={{ color: COLORS.muted }}>store.go:42</div>
    <div className={mono} style={{ color: COLORS.accent }}>
      func <span className="font-bold" style={{ textDecoration: 'underline wavy', textDecorationColor: COLORS.accent }}>Save</span>(a, b, c, d)
    </div>
    <div className="mt-1.5 flex flex-col items-start gap-0.5">
      <span className="rounded px-1 text-[8pt] font-bold text-white" style={{ backgroundColor: COLORS.accent }}>error</span>
      <span className={mono} style={{ color: COLORS.accent }}>too-many-parameters</span>
    </div>
    <div className="text-[8.5pt]" style={{ color: COLORS.muted }}>Save has 4 parameters;<br />maximum is 3</div>
  </Panel>;
}

function QuerySnippet() {
  return <Panel>
    <pre className={mono} style={{ color: COLORS.accent }}>{`SELECT symbol.name,
       line
FROM callers(
  method:Store.Save)
LIMIT 20`}</pre>
    <div className={`mt-1.5 grid grid-cols-[1fr_auto] gap-x-3 border-t pt-1 ${mono}`} style={{ borderColor: COLORS.primary, color: COLORS.muted }}>
      <span>Handler.Post</span><span>18</span>
      <span>Job.Run</span><span>64</span>
    </div>
  </Panel>;
}

function BrowserWindow() {
  return <div className="overflow-hidden rounded-md border bg-white" style={{ borderColor: COLORS.primary }}>
    <div className="flex items-center gap-1 px-2 py-1" style={{ backgroundColor: COLORS.background }}>
      {[0, 1, 2].map((dot) => <span key={dot} className="h-1.5 w-1.5 rounded-full" style={{ backgroundColor: COLORS.muted }} />)}
      <span className={`ml-2 ${mono}`} style={{ color: COLORS.muted }}>localhost:8080</span>
    </div>
    <div className="flex gap-3 border-b px-2 py-1 text-[8.5pt]" style={{ borderColor: COLORS.primary, color: COLORS.muted }}>
      <span className="font-bold" style={{ color: COLORS.accent }}>Symbols</span><span>Files</span><span>Graph</span>
    </div>
    <div className={`px-2 py-1.5 ${mono}`} style={{ color: COLORS.accent }}>
      <div style={{ color: COLORS.muted }}>POST</div>
      <div>/api/v1/modules/query</div>
    </div>
  </div>;
}

function SnapshotChain() {
  const snapshot = (ordinal: number, head = false) =>
    <span key={ordinal} className={`rounded border px-1.5 py-0.5 ${mono}`}
      style={{ borderColor: COLORS.primary, backgroundColor: head ? COLORS.primary : 'white', color: head ? 'white' : COLORS.accent }}>
      #{ordinal}{head ? ' head' : ''}
    </span>;
  const base = (key: string) => <span key={key} className={mono} style={{ color: COLORS.muted }}>←</span>;
  const delta = (operation: string, target: string, color: string) =>
    <div key={target} className={`grid grid-cols-[48px_1fr] ${mono}`} style={{ color }}>
      <span className="font-bold">{operation}</span><span>{target}</span>
    </div>;
  return <Panel>
    <div className="flex items-center gap-1">
      {snapshot(41)}{base('base-41')}{snapshot(42)}{base('base-42')}{snapshot(43, true)}
    </div>
    <div className={`mb-0.5 mt-1.5 border-t pt-1 ${mono}`} style={{ borderColor: COLORS.primary, color: COLORS.muted }}>
      #43 stores only its deltas
    </div>
    {delta('set', 'store.go → rev 9f3a…', COLORS.outputBorder)}
    {delta('delete', 'legacy.go', COLORS.muted)}
    {delta('set', 'Store.Save fingerprints', COLORS.accent)}
  </Panel>;
}

function SnapshotStore({ id }: { id: string }) {
  return <Block id={id} title="Snapshot store" command="uir prune" width="290px">
    <SnapshotChain />
    <div className="mt-2 grid grid-cols-2 gap-1.5">
      <NodePill>SQLite</NodePill>
      <NodePill>PostgreSQL</NodePill>
    </div>
  </Block>;
}

type SourceSpec = { key: string; Icon?: IconComponent; glyph?: string; name: string; detail?: string };

const sources: SourceSpec[] = [
  { key: 'go', Icon: Go, name: 'Go' },
  { key: 'python', Icon: Python, name: 'Python' },
  { key: 'java', Icon: Java, name: 'Java' },
  { key: 'sql', Icon: Postgres, name: 'SQL schema', detail: 'functions + callgraph' },
  { key: 'k8s', Icon: K8S, name: 'Kubernetes' },
  { key: 'oipa', glyph: '</>', name: 'OIPA XML' },
];

function Source({ id, Icon, glyph, name, detail }: { id: string } & Omit<SourceSpec, 'key'>) {
  return <div id={id} className="flex items-center gap-2 rounded-md border bg-white px-2.5 py-1" style={{ borderColor: COLORS.primary, width: '150px' }}>
    {Icon ? <Icon className="h-4 w-4 shrink-0" /> : <span className={`w-4 text-center font-bold ${mono}`} style={{ color: COLORS.accent }}>{glyph}</span>}
    <div className="flex min-w-0 flex-col">
      <span className="text-[9pt] font-semibold" style={{ color: COLORS.accent }}>{name}</span>
      {detail && <span className="whitespace-nowrap text-[7.5pt] leading-tight" style={{ color: COLORS.muted }}>{detail}</span>}
    </div>
  </div>;
}

const PREFERS_REDUCED_MOTION = '(prefers-reduced-motion: reduce)';

function usePrefersReducedMotion(): boolean {
  return React.useSyncExternalStore(
    (notify) => {
      const query = window.matchMedia(PREFERS_REDUCED_MOTION);
      query.addEventListener('change', notify);
      return () => query.removeEventListener('change', notify);
    },
    () => window.matchMedia(PREFERS_REDUCED_MOTION).matches,
    () => true,
  );
}

/*
 * One dot per ~PULSE_PERIOD px of connector, travelling from the source into UIR. It is a dash pattern
 * (a zero-length round-capped dash) animated by Arrow, so the dots follow whatever path the connector takes.
 * Speeds differ slightly per source so the six lanes do not pulse in lockstep.
 */
const PULSE_PERIOD = 90;

function IntoUirPulse({ from, to, index, endOffsetY }: { from: string; to: string; index: number; endOffsetY: number }) {
  const cyclesPerSecond = 0.7 + ((index * 5) % 6) * 0.06;
  return <Arrow variant="primary" from={from} to={to} path="straight" startAnchor="right" showHead={false}
    endAnchor={{ position: 'left', offset: { y: endOffsetY } }} strokeWidth={8} color={COLORS.accent}
    dashness={{ strokeLen: 0.1, nonStrokeLen: PULSE_PERIOD, animation: cyclesPerSecond }}
    arrowBodyProps={{ strokeLinecap: 'round', 'data-uir-pulse': '' }} />;
}

const outputs: Array<{ key: string; command: string; visual: React.ReactNode; caption: string }> = [
  { key: 'query', command: 'uir query', visual: <QuerySnippet />, caption: 'Compact grammar or SQL over symbols, relationships and metrics.' },
  { key: 'graph', command: 'uir graph', visual: <Panel><CallGraph /></Panel>, caption: 'Bounded callers and callees, with the guards around each call.' },
  { key: 'diff', command: 'uir diff', visual: <SymbolDiff />, caption: 'Per symbol, not per line: API, structure or body changes, moves matched, comment-only edits flagged.' },
  { key: 'lint', command: 'uir lint', visual: <LintFinding />, caption: '.uir.yaml SQL rules become Gavel violations at their source line.' },
  { key: 'serve', command: 'uir serve', visual: <BrowserWindow />, caption: 'Snapshot browser, Cmd+K search and the same operations as JSON.' },
];

function Output({ id, command, visual, caption }: {
  id: string; command: string; visual: React.ReactNode; caption: string;
}) {
  return <div id={id} className="flex flex-col" style={{ width: '200px' }}>
    <div className="mb-2 flex">
      <Command color={COLORS.outputBorder}>{command}</Command>
    </div>
    {visual}
    <p className="mt-2 text-[9pt] leading-snug" style={{ color: COLORS.muted }}>{caption}</p>
  </div>;
}

export function OutOfUIR() {
  const reducedMotion = usePrefersReducedMotion();
  return <Diagram className="relative">
    {(id) => <>
      <div className="flex flex-col items-center gap-20 px-6 py-8">
        <div className="grid w-full items-center" style={{ gridTemplateColumns: '1fr auto 1fr', columnGap: '96px' }}>
          <div className="flex flex-col items-end gap-1.5">
            {sources.map(({ key, ...source }) => <Source key={key} id={id(`source-${key}`)} {...source} />)}
          </div>
          <UirHub id={id('uir')} compact />
          <div className="flex justify-start"><SnapshotStore id={id('store')} /></div>
        </div>
        <div className="flex items-start" style={{ gap: '28px' }}>
          {outputs.map((output) => <Output key={output.key} id={id(output.key)}
            command={output.command} visual={output.visual} caption={output.caption} />)}
        </div>
      </div>
      {sources.map((source, index) => <Arrow key={source.key} variant="primary" from={id(`source-${source.key}`)} to={id('uir')}
        path="straight" startAnchor="right" endAnchor={{ position: 'left', offset: { y: (index - 2.5) * 14 } }}
        dashness={reducedMotion ? { strokeLen: 10, nonStrokeLen: 5 } : undefined} />)}
      {!reducedMotion && sources.map((source, index) => <IntoUirPulse key={`pulse-${source.key}`}
        from={id(`source-${source.key}`)} to={id('uir')} index={index} endOffsetY={(index - 2.5) * 14} />)}
      <Arrow variant="secondary" from={id('uir')} to={id('store')} path="straight" startAnchor="right" endAnchor="left"
        labels={{ middle: <NodePill>persisted as</NodePill> }} />
      {outputs.map((output, index) => <Arrow key={output.key} variant="primary" from={id('uir')} to={id(output.key)}
        path="straight" startAnchor={{ position: 'bottom', offset: { x: (index - 2) * 80 } }} endAnchor="top" />)}
    </>}
  </Diagram>;
}

export default function HowItWorksPage() {
  return <Page>
    <Section title="Everything lowers into one model">
      <IntoUIR />
    </Section>
    <Section title="One model answers many questions">
      <OutOfUIR />
    </Section>
  </Page>;
}

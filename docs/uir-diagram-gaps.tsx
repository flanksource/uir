import React from 'react';
import BoxNode from '@flanksource/facet/diagram/BoxNode';
import { COLORS } from '@flanksource/facet/diagram/colors';

export type ElementStatus = 'built' | 'planned' | 'retired';

export const STATUS_COLORS: Readonly<Record<ElementStatus, string>> = {
  built: COLORS.primary,
  planned: COLORS.pk,
  retired: COLORS.muted,
};

export type GapStatus = 'proposed' | 'not started' | 'open' | 'deferred' | 'under evaluation';

export type Gap = { id: number; title: string; today: string; endState: string; source: string; evidence: string; status: GapStatus };

export function GapMarker({ gap }: { gap: number }) {
  return <span className="inline-flex h-[11pt] min-w-[11pt] shrink-0 items-center justify-center rounded-full border border-white px-1 text-[8pt] font-bold leading-none text-white"
    style={{ backgroundColor: COLORS.pk }}>{gap}</span>;
}

export function StatusBox({ id, title, status, gap, minWidth = '250px', children }: {
  id: string; title: string; status: ElementStatus; gap?: number; minWidth?: string; children: React.ReactNode;
}) {
  if (status !== 'built' && gap === undefined) throw new Error(`StatusBox ${id} is ${status} but has no gap number`);
  const color = STATUS_COLORS[status];
  return <BoxNode id={id} headerColor={color} borderColor={color} bodyColor={COLORS.background} compact minWidth={minWidth}
    dashed={status === 'planned'}
    title={<span className="inline-flex items-center gap-1.5 text-sm">
      <span className={status === 'retired' ? 'line-through' : undefined}>{title}</span>
      {gap !== undefined && <GapMarker gap={gap} />}
    </span>}>
    <div style={{ width: `calc(${minWidth} - 24px)` }}>{children}</div>
  </BoxNode>;
}

function Swatch({ status, label }: { status: ElementStatus; label: string }) {
  const color = STATUS_COLORS[status];
  return <span className="inline-flex items-center gap-1.5">
    <span className="inline-block h-[10pt] w-[16pt] overflow-hidden rounded-sm border-2"
      style={{ borderStyle: status === 'planned' ? 'dashed' : 'solid', borderColor: color, backgroundColor: COLORS.background }}>
      <span className="block h-[4pt]" style={{ backgroundColor: color }} />
    </span>
    {label}
  </span>;
}

export function Legend() {
  return <div className="flex flex-wrap items-center text-[8.5pt]" style={{ color: COLORS.muted, columnGap: '16px', rowGap: '4px' }}>
    <Swatch status="built" label="Built: exists today and stays" />
    <Swatch status="planned" label="Planned (dashed): in the end state, absent today" />
    <Swatch status="retired" label="Retired (struck through): removed in the end state" />
    <span className="inline-flex items-center gap-1.5"><GapMarker gap={1} />Numbered marker: see the gap list</span>
  </div>;
}

export function GapList({ gaps }: { gaps: Gap[] }) {
  const seen = new Set<number>();
  for (const gap of gaps) {
    if (seen.has(gap.id)) throw new Error(`GapList has duplicate gap id ${gap.id}`);
    seen.add(gap.id);
  }
  const label = 'font-semibold';
  return <ol className="flex flex-col gap-2 text-[9pt] leading-snug" style={{ color: COLORS.accent }}>
    {gaps.map((gap) => <li key={gap.id} className="grid grid-cols-[22px_1fr] gap-x-1.5 border-t pt-2" style={{ borderColor: COLORS.background }}>
      <span className="pt-0.5"><GapMarker gap={gap.id} /></span>
      <div className="flex flex-col gap-0.5">
        <div className="flex items-baseline gap-2">
          <span className="font-bold">{gap.title}</span>
          <span className="rounded px-1 text-[8pt]" style={{ backgroundColor: COLORS.background, color: COLORS.muted }}>{gap.status}</span>
        </div>
        <div><span className={label}>Today: </span>{gap.today}</div>
        <div><span className={label}>End state: </span>{gap.endState}</div>
        <div className="font-mono text-[8.5pt]" style={{ color: COLORS.muted }}>{gap.source} · {gap.evidence}</div>
      </div>
    </li>)}
  </ol>;
}

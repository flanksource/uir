import type { ComponentProps, ReactNode } from "react";

export function PageLayout({ children }: { children: ReactNode }) {
  return <div className="flex min-w-0 flex-col gap-4 p-5">{children}</div>;
}

export function Section({ children }: { children: ReactNode }) {
  return <section className="flex min-w-0 flex-col gap-3">{children}</section>;
}

export function Row({ children }: { children: ReactNode }) {
  return <div className="flex flex-wrap items-end gap-3">{children}</div>;
}

export function Field({ label, children }: { label: string; children: ReactNode }) {
  return <label className="flex min-w-48 flex-1 flex-col gap-1 text-sm">{label}{children}</label>;
}

export function TextInput(props: ComponentProps<"input">) {
  return <input className="h-9 w-full rounded-md border border-input bg-background px-3 text-sm text-foreground" {...props} />;
}

export function Card({ children }: { children: ReactNode }) {
  return <div className="flex min-w-0 flex-col gap-3 rounded-lg border border-border bg-card p-4">{children}</div>;
}

export function PanelForm({ children, onSubmit, layout = "column" }: {
  children: ReactNode;
  onSubmit: ComponentProps<"form">["onSubmit"];
  layout?: "column" | "row";
}) {
  return <form className={`min-w-0 rounded-lg border border-border bg-card p-4 flex gap-3 ${layout === "row" ? "flex-wrap items-end" : "flex-col"}`} onSubmit={onSubmit}>{children}</form>;
}

export function Heading({ children }: { children: ReactNode }) {
  return <h1 className="m-0 text-2xl font-semibold">{children}</h1>;
}

export function Muted({ children }: { children: ReactNode }) {
  return <span className="text-sm text-muted-foreground">{children}</span>;
}

export function CodeBlock({ children }: { children: ReactNode }) {
  return <pre className="max-h-[35rem] overflow-auto whitespace-pre rounded-md bg-muted p-4 font-mono text-xs">{children}</pre>;
}

export function Detail({ label, children }: { label: string; children: ReactNode }) {
  return <div className="break-all"><dt className="mt-2 text-xs text-muted-foreground">{label}</dt><dd className="mt-0.5">{children}</dd></div>;
}

export function DetailGrid({ children }: { children: ReactNode }) {
  return <dl className="grid grid-cols-1 gap-3 break-all md:grid-cols-2 xl:grid-cols-3">{children}</dl>;
}

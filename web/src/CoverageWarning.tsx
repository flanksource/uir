import { Callout } from "@flanksource/clicky-ui/data";
import type { MissingHead, ModuleQueryCoverage } from "./api";

export function MissingHeads({ missing }: { missing: MissingHead[] }) {
  if (!missing.length) return null;
  return <Callout variant="warning" label="Missing index" title={`${missing.length} ${missing.length === 1 ? "location has" : "locations have"} no current head; queries skip ${missing.length === 1 ? "it" : "them"}`}>
    <ul className="m-0 list-none p-0 text-xs">
      {missing.map((item) => <li key={`${item.root_key}:${item.location}`} className="break-all">
        <code>{item.root_key}</code> · {item.location} · {item.message}
      </li>)}
    </ul>
  </Callout>;
}

export function CoverageWarning({ coverage }: { coverage: ModuleQueryCoverage[] }) {
  if (!coverage.length) return null;
  return <Callout variant="warning" label="Coverage" title={`${coverage.length} ${coverage.length === 1 ? "package" : "packages"} not fully indexed`}>
    <ul className="m-0 list-none p-0 text-xs">
      {coverage.map((item) => <li key={`${item.snapshot_id}:${item.package_path}`} className="break-all">
        <code>{item.package_path}</code> · {item.coverage}{item.diagnostics.length > 0 ? ` · ${item.diagnostics.length} ${item.diagnostics.length === 1 ? "diagnostic" : "diagnostics"}` : ""}
      </li>)}
    </ul>
  </Callout>;
}

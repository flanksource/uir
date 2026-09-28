import { Callout } from "@flanksource/clicky-ui/data";
import type { MissingHeadWarning, ModuleQueryCoverage } from "./api";

export function MissingHeadWarnings({ warnings }: { warnings: MissingHeadWarning[] }) {
  if (!warnings.length) return null;
  return <Callout variant="warning" label="Missing index" title={`${warnings.length} ${warnings.length === 1 ? "checkout" : "checkouts"} skipped`}>
    <ul className="m-0 list-none p-0 text-xs">
      {warnings.map((warning) => <li key={`${warning.root_key}:${warning.location}`} className="break-all">
        <code>{warning.root_key}</code> · {warning.location} · {warning.message}
      </li>)}
    </ul>
  </Callout>;
}

export function CoverageWarning({ coverage }: { coverage: ModuleQueryCoverage[] }) {
  if (!coverage.length) return null;
  return <Callout variant="warning" label="Coverage" title={`${coverage.length} ${coverage.length === 1 ? "package" : "packages"} not fully indexed`}>
    <ul className="m-0 list-none p-0 text-xs">
      {coverage.map((item) => <li key={`${item.snapshot_id}:${item.package_path}`} className="break-all">
        <code>{item.package_path}</code> · {item.coverage}{item.diagnostics > 0 ? ` · ${item.diagnostics} ${item.diagnostics === 1 ? "diagnostic" : "diagnostics"}` : ""}
      </li>)}
    </ul>
  </Callout>;
}

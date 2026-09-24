import { useState } from "react";
import { DropdownMenu } from "@flanksource/clicky-ui/components";
import { UiInfo } from "@flanksource/clicky-ui/icons";
import { getSystemInfo, type SystemInfo } from "./api";
import { useLoad } from "./use-load";

const frontendVersion = import.meta.env.VITE_UIR_VERSION ?? "dev";

function formatSize(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  const unit = Math.min(Math.floor(Math.log(bytes) / Math.log(1024)), 4);
  return `${(bytes / 1024 ** unit).toFixed(1)} ${["B", "KiB", "MiB", "GiB", "TiB"][unit]}`;
}

export function SystemDetails() {
  const [open, setOpen] = useState(false);
  const info = useLoad<SystemInfo>(open ? getSystemInfo : null, open ? "open" : "closed");

  return <DropdownMenu label={<span className="system-details-label">System details</span>} icon={UiInfo} hideChevron variant="ghost" size="sm"
    title="System details" align="left" menuLabel="System details" className="system-details-trigger w-full" menuClassName="w-80 max-w-[calc(100vw-1rem)]"
    onOpenChange={setOpen}>
    {() => <div className="px-3 py-2 text-sm">
      <h2 className="mb-2 font-semibold">System details</h2>
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1">
        <dt className="text-muted-foreground">Frontend</dt><dd>{frontendVersion}</dd>
        {info.data && <>
          <dt className="text-muted-foreground">Backend</dt><dd>{info.data.backend_version}</dd>
          <dt className="text-muted-foreground">Database</dt><dd>{info.data.database_type} {info.data.database_version}</dd>
          <dt className="text-muted-foreground">Location</dt><dd className="min-w-0 break-all">{info.data.database_location}</dd>
          <dt className="text-muted-foreground">Size</dt><dd>{formatSize(info.data.database_size_bytes)}</dd>
        </>}
      </dl>
      {info.loading && <p role="status" className="mt-2 text-muted-foreground">Loading database details…</p>}
      {info.error && <p role="alert" className="mt-2 text-destructive">{info.error}</p>}
    </div>}
  </DropdownMenu>;
}

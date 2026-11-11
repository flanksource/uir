import { afterEach, expect, it, vi } from "vitest";
import { compareGitRevisions } from "../../../web/src/api";

afterEach(() => vi.unstubAllGlobals());

it("preserves rename provenance in comparison responses", async () => {
  const row = { class: "renamed", kind: "func", name_before: "Before", name: "After", visibility: "exported", path_before: "old.go", path_after: "new.go" };
  const result = { root_key: "example.org/app", packages: [{ path: "example.org/app", files: [{ path: "new.go", rows: [row] }] }] };
  vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(JSON.stringify(result))));
  await expect(compareGitRevisions("example.org/app", "old", "new", "all", false)).resolves.toMatchObject(result);
});

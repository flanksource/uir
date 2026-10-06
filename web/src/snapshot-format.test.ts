import { describe, expect, it } from "vitest";
import { snapshotDelta, snapshotDuration, snapshotDurationMs, snapshotSize } from "./snapshot-format";

const base = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee";
const completedAt = "2026-10-01T10:01:30Z";

describe("snapshotDuration", () => {
  it.each([
    ["minutes for a 90 second index", "2026-10-01T10:00:00Z", "1.5 min"],
    ["milliseconds below one second", "2026-10-01T10:01:29.150Z", "850 ms"],
  ])("formats %s", (_, indexStartedAt, expected) => {
    expect(snapshotDuration({ index_started_at: indexStartedAt, completed_at: completedAt })).toBe(expected);
  });

  it("shows a dash for a snapshot published before index start times were recorded", () => {
    expect([snapshotDuration({ completed_at: completedAt }), snapshotDurationMs({ completed_at: completedAt })]).toEqual(["—", undefined]);
  });

  it("sorts by the elapsed milliseconds", () => {
    expect(snapshotDurationMs({ index_started_at: "2026-10-01T10:01:00Z", completed_at: completedAt })).toBe(30_000);
  });

  it.each([
    ["completes before it starts", "2026-10-01T10:02:00Z", completedAt],
    ["has an unparseable start", "yesterday", completedAt],
  ])("rejects a snapshot that %s", (_, indexStartedAt, completed) => {
    expect(() => snapshotDurationMs({ index_started_at: indexStartedAt, completed_at: completed })).toThrow(/snapshot duration/);
  });
});

describe("snapshotSize", () => {
  it.each([
    ["counts and binary-scaled bytes", { file_count: 12, symbol_count: 340, source_bytes: 1536 }, "12 files · 340 symbols · 1.5 KB"],
    ["singular units", { file_count: 1, symbol_count: 1, source_bytes: 900 }, "1 file · 1 symbol · 900 B"],
  ])("renders %s", (_, row, expected) => {
    expect(snapshotSize(row)).toBe(expected);
  });
});

describe("snapshotDelta", () => {
  const counts = { files_added: 2, files_changed: 3, files_deleted: 1, symbols_changed: 14 };

  it("renders file and symbol changes against the short base snapshot id", () => {
    expect(snapshotDelta({ ...counts, base_snapshot_id: base })).toBe("+2 ~3 −1 files · 14 symbols vs aaaaaaaa-bbb");
  });

  it("says when a snapshot has no base to compare against", () => {
    expect(snapshotDelta({ files_added: 40, files_changed: 0, files_deleted: 0, symbols_changed: 1 })).toBe("+40 ~0 −0 files · 1 symbol (no base)");
  });
});

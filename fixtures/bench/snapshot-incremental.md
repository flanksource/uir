---
exec: bash
args: ["-c", 'exec "$SNAPSHOT_RUNNER" case "$SNAPSHOT_CASE"']
timeout: 60m
goProfiles:
  cpu:
    file: cpu.pprof
    env: GAVEL_CPU_PROFILE
  mem:
    file: mem.pprof
    env: GAVEL_MEM_PROFILE
---

# One incremental snapshot observation

The versioned runner prepares the checkout and descriptor before invoking this fixture. Each invocation contains one observation, preserving commit order. `SNAPSHOT_RUNNER` names the compiled runner and `SNAPSHOT_CASE` names the case JSON. Profiling and SQL capture are enabled by `gavel fixtures --benchmark --profile`.

| Name | Exit Code | CEL |
| --- | --- | --- |
| Incremental Git snapshot | 0 | dyn(json).ok == true && dyn(json).source_verified == size(dyn(json).modules) && size(dyn(json).modules) > 0 && size(dyn(json).snapshots) == size(dyn(json).modules) && dyn(json).snapshots.all(s, s.git_commit == dyn(json).case.Commit) && ((dyn(json).case.Path == 'historical' && dyn(json).case.Kind != 'initial') ? dyn(json).before.heads == dyn(json).after.heads : true) && ((dyn(json).case.Kind in ['noop', 'repeat']) ? (dyn(json).modules.all(m, m.unchanged) && dyn(json).delta.all(t, dyn(json).delta[t] == 0)) : true) |

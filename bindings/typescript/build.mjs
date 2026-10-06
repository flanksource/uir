import { spawnSync } from 'node:child_process';
import { copyFileSync } from 'node:fs';

const result = spawnSync('tsc', [], { stdio: 'inherit' });
if (result.error) throw result.error;
if (result.status !== 0) process.exit(result.status ?? 1);
copyFileSync('model.d.ts', 'dist/model.d.ts');

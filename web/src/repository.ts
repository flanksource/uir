export function repositoryURL(repository: string): string | null {
  if (/^https?:\/\//.test(repository)) return repository;
  const scp = /^git@([^:/]+):(.+?)(?:\.git)?$/.exec(repository);
  if (scp) return `https://${scp[1]}/${scp[2].replace(/\.git$/, "")}`;
  const ssh = /^ssh:\/\/git@([^/]+)\/(.+?)(?:\.git)?$/.exec(repository);
  if (ssh) return `https://${ssh[1]}/${ssh[2].replace(/\.git$/, "")}`;
  return null;
}

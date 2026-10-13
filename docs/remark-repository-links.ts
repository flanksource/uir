import { statSync } from 'node:fs'
import { dirname, relative, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import type { Root } from 'mdast'
import { visit } from 'unist-util-visit'

const repositoryRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')

export function resolveDocumentationLink(
  url: string,
  sourcePath: string,
  pageRoutes: ReadonlyMap<string, string>,
): string {
  if (/^(?:[a-z][a-z\d+.-]*:|\/|#)/i.test(url)) return url
  const [path, suffix = ''] = url.split(/(?=[?#])/, 2)
  const target = resolve(dirname(sourcePath), decodeURI(path))
  const repositoryPath = relative(repositoryRoot, target)
  if (repositoryPath.startsWith('..')) throw new Error(`Link outside repository: ${url}`)
  const route = pageRoutes.get(target)
  if (route) return `${route}${suffix}`
  const kind = statSync(target).isDirectory() ? 'tree' : 'blob'
  return `https://github.com/flanksource/uir/${kind}/main/${repositoryPath}${suffix}`
}

export default function remarkRepositoryLinks(pageRoutes: ReadonlyMap<string, string>) {
  return (tree: Root, file: { path: string }) => {
    const sourcePath = file.path
    visit(tree, ['link', 'definition'], (node) => {
      if (node.type === 'link' || node.type === 'definition')
        node.url = resolveDocumentationLink(node.url, sourcePath, pageRoutes)
    })
  }
}

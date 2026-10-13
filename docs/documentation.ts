import { mkdirSync, readFileSync, readdirSync, writeFileSync, existsSync, unlinkSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { unified } from 'unified'
import remarkParse from 'remark-parse'
import remarkGfm from 'remark-gfm'
import remarkStringify from 'remark-stringify'
import remarkRepositoryLinks from './remark-repository-links.ts'

export const docsDirectory = dirname(fileURLToPath(import.meta.url))
export const repositoryRoot = resolve(docsDirectory, '..')
export const documentation = new Map([
  ...readdirSync(docsDirectory)
    .filter((name) => name.endsWith('.md') &&
      !name.endsWith('-plan.md') && !name.endsWith('-analysis.md') &&
      name !== 'README.md' && name !== 'symbol-reference-storage.md')
    .map((name) => [resolve(docsDirectory, name), `/${name.slice(0, -3)}`] as const),
  [resolve(repositoryRoot, 'UIR.md'), '/model'],
  [resolve(repositoryRoot, 'uir-api.md'), '/architecture'],
  [resolve(repositoryRoot, 'storage/README.md'), '/storage'],
])

export function renderDocumentation(source: { path: string; value: string }) {
  const content = String(unified()
    .use(remarkParse)
    .use(remarkGfm)
    .use(remarkRepositoryLinks, documentation)
    .use(remarkStringify, {
      handlers: {
        text(node, _parent, state, info) {
          return state.safe(node.value, info).replace(/[<>{}]/g,
            (character) => `&#${character.charCodeAt(0)};`)
        },
      },
    })
    .processSync(source))

  if (source.path === resolve(docsDirectory, 'module-indexing.md')) {
    const heading = '\n## Storage hierarchy\n'
    if (!content.includes(heading)) throw new Error(`Missing Storage hierarchy heading in ${source.path}`)
    return "import { PublicationDiagram } from '../components/Diagrams'\n\n" +
      content.replace(heading, `${heading}\n<PublicationDiagram />\n`)
  }
  return content
}

export function prepareDocumentation() {
  const pagesDirectory = resolve(docsDirectory, 'src/pages')
  mkdirSync(pagesDirectory, { recursive: true })
  const generated = new Set<string>()
  for (const [source, route] of documentation) {
    const extension = source === resolve(docsDirectory, 'module-indexing.md') ? 'mdx' : 'md'
    const name = `${route.slice(1)}.${extension}`
    generated.add(name)
    const target = resolve(pagesDirectory, name)
    const content = renderDocumentation({ path: source, value: readFileSync(source, 'utf8') })
    if (!existsSync(target) || readFileSync(target, 'utf8') !== content)
      writeFileSync(target, content)
  }
  for (const name of readdirSync(pagesDirectory)) {
    if ((name.endsWith('.md') || name === 'module-indexing.mdx') && !generated.has(name))
      unlinkSync(resolve(pagesDirectory, name))
  }
}

import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { resolveDocumentationLink } from './remark-repository-links'

const root = resolve(import.meta.dirname, '..')
const source = resolve(root, 'docs/symbols.md')
const routes = new Map([
  [resolve(root, 'docs/query.md'), '/query'],
  [resolve(root, 'storage/README.md'), '/storage'],
])

describe('repository documentation links', () => {
  it.each([
    ['query.md', '/query'],
    ['query.md#results-and-limits', '/query#results-and-limits'],
    ['../storage/README.md#compact-symbol-handles', '/storage#compact-symbol-handles'],
    ['../query/module_index_ops.go', 'https://github.com/flanksource/uir/blob/main/query/module_index_ops.go'],
    ['examples/import', 'https://github.com/flanksource/uir/tree/main/docs/examples/import'],
    ['#reference-forms', '#reference-forms'],
    ['/graph', '/graph'],
    ['https://example.org/docs', 'https://example.org/docs'],
    ['mailto:docs@example.org', 'mailto:docs@example.org'],
  ])('resolves %s to %s', (input, expected) => {
    expect(resolveDocumentationLink(input, source, routes)).toBe(expected)
  })

  it('rejects a missing repository link', () => {
    expect(() => resolveDocumentationLink('missing.md', source, routes)).toThrow('missing.md')
  })

  it('rejects a link outside the repository', () => {
    expect(() => resolveDocumentationLink('../../outside.md', source, routes)).toThrow('outside')
  })
})

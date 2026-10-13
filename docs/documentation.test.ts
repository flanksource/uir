import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { docsDirectory, renderDocumentation } from './documentation.ts'

describe('documentation preparation', () => {
  it('embeds the live publication component beneath the storage hierarchy heading', () => {
    expect(renderDocumentation({
      path: resolve(docsDirectory, 'module-indexing.md'),
      value: '# Module indexing\n\n## Storage hierarchy\n\nExisting storage documentation.\n',
    })).toBe("import { PublicationDiagram } from '../components/Diagrams'\n\n# Module indexing\n\n## Storage hierarchy\n\n<PublicationDiagram />\n\nExisting storage documentation.\n")
  })

  it('fails when the publication diagram has no matching section', () => {
    expect(() => renderDocumentation({
      path: resolve(docsDirectory, 'module-indexing.md'),
      value: '# Module indexing\n',
    })).toThrow('Missing Storage hierarchy heading')
  })

  it('escapes MDX syntax in prose while preserving Go examples', () => {
    expect(renderDocumentation({
      path: resolve(docsDirectory, 'indexing.md'),
      value: '# Channels\n\nSend ch <- value and use {field}.\n\n```go\nch <- value\n```\n',
    })).toBe('# Channels\n\nSend ch &#60;- value and use &#123;field&#125;.\n\n```go\nch <- value\n```\n')
  })

  it('rewrites reference-style links without rewriting links inside code blocks', () => {
    const content = renderDocumentation({
      path: resolve(docsDirectory, 'indexing.md'),
      value: '[Queries][query]\n\n[query]: query.md#results-and-limits\n\n```text\n[Queries](query.md)\n```\n',
    })
    expect(content).toContain('[query]: /query#results-and-limits')
    expect(content).toContain('```text\n[Queries](query.md)\n```')
  })
})

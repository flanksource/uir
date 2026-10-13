import { defineConfig } from 'vocs/config'

export default defineConfig({
  title: 'UIR',
  description: 'A universal intermediate representation for indexing, querying, and comparing code.',
  renderStrategy: 'full-static',
  topNav: [
    { text: 'Guides', link: '/getting-started' },
    { text: 'Model', link: '/model' },
    { text: 'GitHub', link: 'https://github.com/flanksource/uir', external: true },
  ],
  sidebar: [
    { text: 'Introduction', link: '/' },
    { text: 'Getting started', link: '/getting-started' },
    {
      text: 'Guides',
      items: [
        { text: 'Module indexing', link: '/module-indexing' },
        { text: 'Go AST indexing', link: '/indexing' },
        { text: 'Compact queries', link: '/query' },
        { text: 'SQL navigation', link: '/sql-query' },
        { text: 'Call graphs', link: '/graph' },
        { text: 'Symbol diffs', link: '/diff' },
        { text: 'Historical imports', link: '/historical-import' },
        { text: 'Browser and API', link: '/serve' },
      ],
    },
    {
      text: 'Reference',
      items: [
        { text: 'UIR model', link: '/model' },
        { text: 'Architecture', link: '/architecture' },
        { text: 'Symbols and references', link: '/symbols' },
        { text: 'Storage', link: '/storage' },
        { text: 'Storage ERD', link: '/storage-erd' },
        { text: 'Symbol index storage', link: '/symbol-index-storage' },
        { text: 'Compact symbol handles', link: '/symbol-compact-ids' },
        { text: 'Benchmarks', link: '/benchmarks' },
      ],
    },
  ],
})

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
      text: 'Index and publish',
      items: [
        { text: 'Module indexing', link: '/module-indexing' },
        { text: 'Go indexing', link: '/indexing' },
        { text: 'Git-backed publication', link: '/git-backed-publication' },
        { text: 'Historical imports', link: '/historical-import' },
      ],
    },
    {
      text: 'Explore and check',
      items: [
        { text: 'Compact queries', link: '/query' },
        { text: 'SQL examples', link: '/sql-examples' },
        { text: 'SQL lint rules', link: '/lint' },
        { text: 'Call graphs', link: '/graph' },
        { text: 'Symbol diffs', link: '/diff' },
        { text: 'Test runs and flakiness', link: '/tests' },
        { text: 'Browser and API', link: '/serve' },
      ],
    },
    {
      text: 'Reference',
      items: [
        { text: 'CLI configuration', link: '/cli' },
        { text: 'SQL language', link: '/sql-query' },
        { text: 'UIR model', link: '/model' },
        { text: 'Symbols and references', link: '/symbols' },
      ],
    },
    {
      text: 'Architecture and design',
      items: [
        { text: 'Architecture', link: '/architecture' },
        { text: 'Storage', link: '/storage' },
        { text: 'Storage ERD', link: '/storage-erd' },
        { text: 'Symbol index storage', link: '/symbol-index-storage' },
        { text: 'Compact symbol handles', link: '/symbol-compact-ids' },
        { text: 'Benchmarks', link: '/benchmarks' },
      ],
    },
  ],
})

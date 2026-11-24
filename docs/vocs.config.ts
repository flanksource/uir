import { defineConfig } from 'vocs/config'

const basePath = process.env.UIR_DOCS_BASE_PATH ?? '/'
const brandAssets = `${basePath.replace(/\/$/, '')}/brand`

export default defineConfig({
  title: 'UIR',
  description: 'A universal intermediate representation for indexing, querying, and comparing code.',
  basePath,
  accentColor: 'light-dark(#3578e5, #0ea5e9)',
  iconUrl: `${brandAssets}/favicon.svg`,
  logoUrl: {
    light: `${brandAssets}/uir-horizontal.svg`,
    dark: `${brandAssets}/uir-horizontal-reversed.svg`,
  },
  renderStrategy: 'full-static',
  topNav: [
    { text: 'Guides', link: '/getting-started' },
    { text: 'How it works', link: '/how-it-works' },
    { text: 'Model', link: '/model' },
    { text: 'GitHub', link: 'https://github.com/flanksource/uir', external: true },
  ],
  sidebar: [
    { text: 'Introduction', link: '/overview' },
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
        { text: 'How UIR works', link: '/how-it-works' },
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

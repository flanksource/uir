# Documentation site

The UIR documentation site uses [Vocs](https://vocs.dev/introduction/getting-started), Waku, React, and Vite. It is an independent pnpm package alongside the CLI's `web/` package. Use Node.js 24 and the pnpm version pinned in the repository's root `package.json`.

## Develop

From the repository root:

```bash
pnpm --dir docs install --frozen-lockfile
make docs-dev
```

Open the local URL printed by Vocs, normally `http://localhost:5173`.

## Build and check

```bash
make docs-check
make docs-build
pnpm --dir docs run preview
```

Vocs builds a fully static site under `docs/dist/public`. Serve that directory with a static host. Set `baseUrl` in `vocs.config.ts` when a production URL is chosen; Vocs uses it to generate canonical URLs, the sitemap, and robots metadata. Set `basePath` as well when hosting under a subpath.

## Edit content

Keep editing the existing Markdown guides directly in `docs/`, the model reference in `UIR.md`, the architecture reference in `uir-api.md`, and the storage reference in `storage/README.md`. The `predev` and `prebuild` scripts generate ignored pages under `src/pages/` using `documentation.ts`: `.md` for prose guides and `.mdx` for the module-indexing guide with its React diagram. Restart the dev server after editing these source documents. Relative links to published guides become site routes; links to repository files become GitHub links. A missing repository file fails preparation.

Plans, analyses, and the proposed symbol-reference storage design remain repository documents rather than published guides. Add site-specific pages as `.mdx` under `src/pages/`, and add their navigation entries in `vocs.config.ts`. Generated pages must not be edited or committed.

`remark-repository-links.test.ts` tests route conversion, fragments, repository source links, and invalid links. Vocs also checks internal dead links during the production build.

## Diagrams

The introduction and module-indexing pages render live React diagrams using Facet's `Diagram`, `BoxNode`, and `Arrow` components. Edit `src/components/Diagrams.tsx` to change their content. `documentation.ts` inserts the publication diagram under the generated module guide's storage heading. Narrow screens can scroll each diagram horizontally, including with the keyboard after focusing its frame.

The Storage ERD reference page reuses the seven entity diagrams exported by `uir-gorm-erd.tsx` through `src/components/StorageERD.tsx`. It retains the original proposed-design status and links to the implementation and design references. Edit `src/pages/storage-erd.mdx` for its explanatory text.

`src/components/diagrams.css` generates the Tailwind utilities used by the diagrams without importing Facet's document-wide print styles. The site build bundles the React components; no separate rendering command or image assets are needed.

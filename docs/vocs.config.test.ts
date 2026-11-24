import { afterEach, describe, expect, it, vi } from 'vitest'

describe('docs brand assets', () => {
  afterEach(() => vi.unstubAllEnvs())

  it.each([
    { configuredPath: undefined, basePath: '/', assetPrefix: '/brand' },
    { configuredPath: '/uir', basePath: '/uir', assetPrefix: '/uir/brand' },
  ])('uses $assetPrefix for the configured site', async ({ configuredPath, basePath, assetPrefix }) => {
    vi.stubEnv('UIR_DOCS_BASE_PATH', configuredPath)
    vi.resetModules()
    const { default: config } = await import('./vocs.config.ts')

    expect(config).toMatchObject({
      basePath,
      iconUrl: `${assetPrefix}/favicon.svg`,
      logoUrl: {
        light: `${assetPrefix}/uir-horizontal.svg`,
        dark: `${assetPrefix}/uir-horizontal-reversed.svg`,
      },
    })
  })
})

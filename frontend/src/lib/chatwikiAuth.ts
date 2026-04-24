type LoginParams = {
  os_type?: string
  os_version?: string
}

type BrowserServiceLike = {
  GetLoginParams: () => Promise<LoginParams>
  OpenURL: (url: string) => Promise<void>
}

function tryParseUrl(value: string): URL | null {
  try {
    return new URL(value.trim())
  } catch {
    return null
  }
}

export function isSameChatWikiHost(candidateBase: string, cloudBase: string): boolean {
  const candidateUrl = tryParseUrl(candidateBase)
  const cloudUrl = tryParseUrl(cloudBase)

  if (!candidateUrl || !cloudUrl) return false

  return candidateUrl.hostname.toLowerCase() === cloudUrl.hostname.toLowerCase()
}

export function resolveChatWikiLoginSource(
  candidateBase: string,
  cloudBase: string
): 'cloud' | 'open-source' {
  return isSameChatWikiHost(candidateBase, cloudBase) ? 'cloud' : 'open-source'
}

export function buildChatWikiLoginUrl(base: string, params?: LoginParams): string {
  const normalizedBase = base.replace(/\/+$/, '')
  const path = `${normalizedBase}/#/chatclaw/login`
  const query = new URLSearchParams()

  if (params?.os_type) query.set('os_type', params.os_type)
  if (params?.os_version) query.set('os_version', params.os_version)

  const queryString = query.toString()
  return queryString ? `${path}?${queryString}` : path
}

async function getDefaultBrowserService(): Promise<BrowserServiceLike> {
  const module = await import('@bindings/chatclaw/internal/services/browser')
  return module.BrowserService as BrowserServiceLike
}

export async function openChatWikiCloudLogin(
  base: string,
  browserService?: BrowserServiceLike
): Promise<string> {
  const service = browserService ?? (await getDefaultBrowserService())
  const params = await service.GetLoginParams().catch(() => undefined)
  const url = buildChatWikiLoginUrl(base, params)
  await service.OpenURL(url)
  return url
}

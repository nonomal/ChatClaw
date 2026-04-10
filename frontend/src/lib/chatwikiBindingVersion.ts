type ChatwikiBindingLike = {
  chatwiki_version?: string | null
} | null

export type NormalizedChatwikiAvailability = 'available' | 'unbound' | 'non_cloud'

/**
 * Align with Go chatwiki.normalizeChatWikiVersion: empty means self-hosted / non-cloud ("dev").
 * Without this, a missing or empty chatwiki_version was treated as cloud "available" and skipped
 * the first-launch ChatWiki cloud login reminder incorrectly.
 */
export function normalizeChatwikiVersion(value?: string | null): string {
  const trimmed = (value ?? '').trim()
  if (trimmed === '') return 'dev'
  return trimmed.toLowerCase()
}

export function isChatwikiDevBinding(binding: ChatwikiBindingLike): boolean {
  return normalizeChatwikiVersion(binding?.chatwiki_version) === 'dev'
}

export function getNormalizedChatwikiAvailability(
  binding: ChatwikiBindingLike
): NormalizedChatwikiAvailability {
  if (!binding) return 'unbound'
  return isChatwikiDevBinding(binding) ? 'non_cloud' : 'available'
}

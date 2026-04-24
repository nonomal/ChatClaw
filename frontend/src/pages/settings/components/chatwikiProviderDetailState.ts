export type ChatwikiProviderDetailBindingLike = {
  chatwiki_version?: string | null
}

function normalizeText(value?: string | null): string {
  return value?.trim().toLowerCase() || ''
}

export function normalizeChatwikiBindingVersion(
  binding: ChatwikiProviderDetailBindingLike | null
): string {
  if (!binding) return ''
  return normalizeText(binding.chatwiki_version)
}

export function isCloudBinding(binding: ChatwikiProviderDetailBindingLike | null): boolean {
  return normalizeChatwikiBindingVersion(binding) === 'yun'
}

export function isOpenSourceBinding(binding: ChatwikiProviderDetailBindingLike | null): boolean {
  return normalizeChatwikiBindingVersion(binding) === 'dev'
}

export function shouldShowChatwikiAccountCard(
  binding: ChatwikiProviderDetailBindingLike | null
): boolean {
  return binding != null
}

export function shouldShowChatwikiCreditsCard(
  binding: ChatwikiProviderDetailBindingLike | null
): boolean {
  return isCloudBinding(binding)
}

export function canUseChatwikiModelService(
  binding: ChatwikiProviderDetailBindingLike | null
): boolean {
  return isCloudBinding(binding)
}

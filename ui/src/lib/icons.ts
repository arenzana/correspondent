// The app icons a reader can choose. Plain data, shared with the service
// worker, which keeps every choice cached so Settings can always show them.

export type AppIcon = 'wire' | 'byline' | 'morse'

export const APP_ICONS: { id: AppIcon; label: string }[] = [
  { id: 'wire', label: 'Wire' },
  { id: 'byline', label: 'Byline' },
  { id: 'morse', label: 'Morse' },
]

/** The files Settings and the page head use for each icon. */
export const appIconAssets = (): string[] =>
  APP_ICONS.flatMap(({ id }) => ['icon-192.png', 'favicon.svg', 'favicon-32.png'].map((f) => `/icons/${id}/${f}`))

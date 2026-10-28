/**
 * How to scroll from script: smoothly, unless the reader asked the system
 * to reduce motion. CSS's scroll-behavior rule doesn't reach scrollBy and
 * scrollIntoView calls that name their own behaviour.
 */
export function scrollMotion(): ScrollBehavior {
  return typeof matchMedia === 'function' && matchMedia('(prefers-reduced-motion: reduce)').matches ? 'auto' : 'smooth'
}

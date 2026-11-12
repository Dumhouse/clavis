import { clsx, type ClassValue } from 'clsx'
import { extendTailwindMerge } from 'tailwind-merge'

// Teach tailwind-merge the custom type-scale steps (styles.css) so classes
// like text-2xs are treated as font sizes, not colors, and never drop a
// text-muted next to them.
const twMerge = extendTailwindMerge({
  extend: { theme: { text: ['3xs', '2xs', 'md'] } },
})

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

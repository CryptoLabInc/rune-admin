import { clsx, type ClassValue } from "clsx";
import { extendTailwindMerge } from "tailwind-merge";

/* Custom @theme font sizes (src/index.css) must be registered as the
 * font-size group — otherwise tailwind-merge classifies them as text
 * colors and drops them when a text-{color} class follows. */
const twMerge = extendTailwindMerge({
  extend: { classGroups: { "font-size": ["text-tag", "text-md"] } },
});

/** cn merges Tailwind classes with conditional clsx inputs. */
export const cn = (...inputs: ClassValue[]) => twMerge(clsx(inputs));

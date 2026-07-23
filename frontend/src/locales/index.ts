import { en, type TLocale } from "./en";
import { ko } from "./ko";

/** LANG_STORAGE_KEY persists an explicit user language choice; absent, the
 * browser language decides (Korean browsers → ko, everything else → en). */
export const LANG_STORAGE_KEY = "rc_lang";

export type TLanguage = "en" | "ko";

const detectLanguage = (): TLanguage => {
  try {
    const stored = localStorage.getItem(LANG_STORAGE_KEY);
    if (stored === "en" || stored === "ko") return stored;
  } catch {
    /* storage unavailable (private mode) — fall through to browser language */
  }
  return navigator.language?.toLowerCase().startsWith("ko") ? "ko" : "en";
};

/** language is resolved ONCE at page load and never changes within a visit —
 * every string read below is a plain static property access. */
export const language: TLanguage = detectLanguage();

/** L is the active locale table. Import L and read L.btn.close etc.; the
 * value is the same hardcoded string on every access for the whole visit. */
export const L: TLocale = language === "ko" ? ko : en;

/** setLanguage persists the choice and reloads — the locale is bound at page
 * load (see above), so a reload is what applies the new language everywhere. */
export const setLanguage = (lang: TLanguage) => {
  try {
    localStorage.setItem(LANG_STORAGE_KEY, lang);
  } catch {
    /* storage unavailable — reload alone keeps the browser-language default */
  }
  window.location.reload();
};

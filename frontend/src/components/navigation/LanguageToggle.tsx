import { Globe } from "lucide-react";

import { language, setLanguage } from "@/locales";

/**
 * LanguageToggle is the always-visible navbar language switch (globe + label,
 * the top-right convention users scan for). The label names the language it
 * switches TO, in that language's own script, so a user stuck in the wrong
 * language can still recognize the way out. setLanguage persists the choice
 * and reloads — the locale is bound once at page load (see src/locales).
 */
const LanguageToggle = () => {
  const nextLanguage = language === "ko" ? "en" : "ko";
  const nextLanguageLabel = language === "ko" ? "English" : "한국어";

  return (
    <button
      type="button"
      className="text-muted-foreground hover:text-foreground flex cursor-pointer items-center gap-1.5 rounded px-2 py-1 text-sm"
      onClick={() => setLanguage(nextLanguage)}
    >
      <Globe className="size-4" aria-hidden="true" />
      {nextLanguageLabel}
    </button>
  );
};

export default LanguageToggle;

import {
  BADGE_TONE_VAR,
  BTN_COLOR_VAR,
  BTN_SIZE_VAR,
  TEXT_BTN_TONE_VAR,
} from "@/constants/styleConstants";

/** Union types for element style variants (T-prefixed, envector pattern). */
export type TBTNSize = keyof typeof BTN_SIZE_VAR;
export type TBTNColor = keyof typeof BTN_COLOR_VAR;
export type TTextBTNTone = keyof typeof TEXT_BTN_TONE_VAR;
export type TBadgeTone = keyof typeof BADGE_TONE_VAR;

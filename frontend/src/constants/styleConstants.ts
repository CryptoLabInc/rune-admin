/**
 * Style variant maps for shared element components (envector pattern).
 * Visual values are translated from UIKIT modules/rune-ui-buttons and
 * modules/rune-admin-kit CSS — UIKIT is the design source of truth.
 */

/* Form controls embed w-full: the parent container constrains width.
   Metrics are UIKIT values normalized to even px (project rule). */
/* Sizes are height-based: xs 24px / sm 32px / md 36px / lg 42px. */
export const BTN_SIZE_VAR = {
  xs: "h-6 w-full rounded-[6px] px-2 text-xs",
  sm: "h-8 w-full rounded-[8px] px-3 text-sm",
  md: "h-9 w-full rounded-md px-4 text-base",
  lg: "h-[42px] w-full rounded-md px-5 text-lg",
} as const;

/* Theme roles (2026-07-13): filled = mintFilled(primary) /
   grayFilled(secondary) / redFilled(warning); outline =
   mintOutline(primary) / grayOutline(secondary) / redOutline(danger).
   The disabled face is
   shared across every theme — a flat gray fill defined once in the
   Button base styles, so a disabled/loading button always reads the
   same regardless of theme. */
export const BTN_COLOR_VAR = {
  /* UIKIT primary — precision flat (2026-07-12 rework): one plane, one
     color, plus a machined 1px inner edge; hover brightens with a 3px
     ring bloom; press darkens with a 0.985 micro-scale. */
  mintFilled:
    "bg-mint text-on-mint shadow-[inset_0_0_0_1px_rgba(4,36,31,0.18)] hover:enabled:bg-mint-hot hover:enabled:shadow-[inset_0_0_0_1px_rgba(4,36,31,0.14),0_0_0_3px_color-mix(in_srgb,var(--color-mint)_14%,transparent)] focus-visible:bg-mint-hot focus-visible:shadow-[inset_0_0_0_1px_rgba(4,36,31,0.14),0_0_0_3px_color-mix(in_srgb,var(--color-mint)_14%,transparent)] active:enabled:bg-mint-deep active:enabled:scale-[0.985] active:enabled:shadow-[inset_0_0_0_1px_rgba(4,36,31,0.2)]",
  /* UIKIT danger — destructive confirm, last button of a confirm dialog
     only. Same flat grammar as primary in red. */
  redFilled:
    "bg-negative text-on-negative shadow-[inset_0_0_0_1px_rgba(58,8,14,0.2)] hover:enabled:bg-[color-mix(in_srgb,var(--color-negative)_88%,#fff)] hover:enabled:shadow-[inset_0_0_0_1px_rgba(58,8,14,0.16),0_0_0_3px_color-mix(in_srgb,var(--color-negative)_14%,transparent)] focus-visible:bg-[color-mix(in_srgb,var(--color-negative)_88%,#fff)] focus-visible:shadow-[inset_0_0_0_1px_rgba(58,8,14,0.16),0_0_0_3px_color-mix(in_srgb,var(--color-negative)_14%,transparent)] active:enabled:bg-[color-mix(in_srgb,var(--color-negative)_82%,#000)] active:enabled:scale-[0.985]",
  /* UIKIT ghost, flattened to the filled grammar (2026-07-13 — UIKIT's
     ghost still carries the glass gradient + hover glow/lift): quiet
     face + hairline border; hover stays gray (border/face deepen — a
     mint recolor read identical to mintOutline's hover); press
     micro-scales. */
  grayOutline:
    "border-border-strong bg-muted-foreground/5 light:bg-white text-foreground hover:enabled:border-muted-foreground/60 hover:enabled:bg-muted-foreground/10 focus-visible:border-muted-foreground/60 focus-visible:bg-muted-foreground/10 active:enabled:scale-[0.985] active:enabled:bg-muted-foreground/14",
  /* Outline primary — same flat outline grammar as grayOutline but
     mint at rest (no UIKIT source, added 2026-07-13). */
  mintOutline:
    "border-mint/50 bg-mint/5 light:bg-white text-mint hover:enabled:border-mint/75 hover:enabled:bg-mint/10 hover:enabled:text-mint-hot focus-visible:border-mint/75 focus-visible:bg-mint/10 focus-visible:text-mint-hot active:enabled:scale-[0.985] active:enabled:bg-mint/14",
  /* Outline danger — same flat outline grammar as mintOutline in red;
     for destructive actions that shouldn't carry a filled emphasis
     (no negative "hot" token, so the label stays text-negative and only
     the border/face deepen on hover). */
  redOutline:
    "border-negative/50 bg-negative/5 light:bg-white text-negative hover:enabled:border-negative/75 hover:enabled:bg-negative/10 focus-visible:border-negative/75 focus-visible:bg-negative/10 active:enabled:scale-[0.985] active:enabled:bg-negative/14",
  /* UIKIT soft — quiet secondary fill */
  grayFilled:
    "bg-muted-foreground/8 text-muted-foreground hover:enabled:bg-muted-foreground/14 hover:enabled:text-foreground focus-visible:bg-muted-foreground/14 focus-visible:text-foreground",
} as const;

/* The shared hover/focus-visible "hot" face per theme, unprefixed —
   the /ui-test showcase uses it to freeze that state for comparison.
   Keep in sync with the hover:/focus-visible: classes above (Tailwind
   extracts class names statically, so the variants cannot be composed
   from this map at runtime). */
export const BTN_HOT_VAR = {
  mintFilled:
    "bg-mint-hot shadow-[inset_0_0_0_1px_rgba(4,36,31,0.14),0_0_0_3px_color-mix(in_srgb,var(--color-mint)_14%,transparent)]",
  redFilled:
    "bg-[color-mix(in_srgb,var(--color-negative)_88%,#fff)] shadow-[inset_0_0_0_1px_rgba(58,8,14,0.16),0_0_0_3px_color-mix(in_srgb,var(--color-negative)_14%,transparent)]",
  grayOutline: "border-muted-foreground/60 bg-muted-foreground/10",
  mintOutline: "border-mint/75 bg-mint/10 text-mint-hot",
  redOutline: "border-negative/75 bg-negative/10",
  grayFilled: "bg-muted-foreground/14 text-foreground",
} as const;

export const TEXT_BTN_TONE_VAR = {
  gray: "text-faint hover:enabled:text-foreground",
  red: "text-[color-mix(in_srgb,var(--color-negative)_72%,var(--color-muted-foreground))] hover:enabled:text-negative",
} as const;

export const BADGE_TONE_VAR = {
  accent: "bg-mint text-on-mint shadow-[inset_0_0_0_1px_rgba(4,36,31,0.18)]",
  neutral: "bg-muted-foreground/12 text-muted-foreground",
} as const;

/* Session chips — the only status a list view shows. */
export const MEMBER_STATUS_VAR = {
  online: { label: "온라인", color: "text-mint" },
  offline: { label: "오프라인", color: "text-faint" },
} as const;

/* Invitation-status labels — shown only in the member detail drawer. */
export const INVITATION_STATUS_VAR = {
  invite_pending: { label: "초대 수락 대기", color: "text-warning" },
  invite_expired: { label: "초대 코드 만료", color: "text-faint" },
  invite_redeemed: { label: "초대 코드 사용됨", color: "text-accent-blue" },
} as const;

export const WORKSPACE_STATUS_VAR = {
  provisioning: { label: "생성 중", color: "text-warning" },
  running: { label: "실행 중", color: "text-mint" },
  stopping: { label: "정지 중", color: "text-warning" },
  stopped: { label: "정지", color: "text-faint" },
  starting: { label: "재실행 중", color: "text-warning" },
  deleting: { label: "삭제 중", color: "text-warning" },
  error: { label: "사용 불가", color: "text-negative" },
} as const;

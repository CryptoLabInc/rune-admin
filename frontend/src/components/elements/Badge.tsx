import { cn } from "@/utils/cn";
import { BADGE_TONE_VAR } from "@/constants/styleConstants";
import type { TBadgeTone } from "@/types/styleTypes";

const styles = {
  default:
    "inline-flex h-6 w-fit cursor-pointer items-center justify-center rounded-full px-2 font-mono text-tag font-semibold tracking-[0.02em]",
};

interface BadgeProps {
  value: number;
  max?: number;
  tone?: TBadgeTone;
  className?: string;
}

/**
 * Badge is the numeric count badge (ported from UIKIT AdminBadge) —
 * nav pending-invite counts, selection counts. Values above max clamp
 * to "99+"; zero or below renders nothing.
 */
const Badge = ({ value, max = 99, tone = "accent", className }: BadgeProps) => {
  if (value <= 0) return null;
  return (
    <span className={cn(styles.default, BADGE_TONE_VAR[tone], className)}>
      {value > max ? `${max}+` : value}
    </span>
  );
};

export default Badge;

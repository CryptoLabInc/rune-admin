import { cn } from "@/utils/cn";

interface StatusBadgeProps {
  label: string;
  /** Tailwind text-color class; the dot inherits it via bg-current. */
  color: string;
  className?: string;
}

/**
 * StatusBadge is the shared status chip (colored dot + Korean label),
 * ported from UIKIT MemberStatus. Drives both the session-status chip
 * (MemberStatus) and the invitation-status chip in the member drawer so
 * the two axes render in one identical badge style.
 */
const StatusBadge = ({ label, color, className }: StatusBadgeProps) => {
  return (
    <span
      className={cn(
        "inline-flex h-8 w-fit cursor-pointer items-center gap-2 p-1 text-sm whitespace-nowrap",
        color,
        className,
      )}
    >
      <span aria-hidden="true" className="size-1 rounded-full bg-current" />
      {label}
    </span>
  );
};

export default StatusBadge;

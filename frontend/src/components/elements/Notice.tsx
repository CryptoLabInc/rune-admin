import type { ReactNode } from "react";

import IconAlert from "@/components/icons/IconAlert";
import IconCheck from "@/components/icons/IconCheck";
import IconInfo from "@/components/icons/IconInfo";
import { cn } from "@/utils/cn";

const styles = {
  wrap: "bg-surface text-muted-foreground grid grid-cols-[20px_1fr] items-start gap-2.5 rounded-md border px-3 py-3 text-sm leading-[1.5]",
  wrapError: "border-negative/20 bg-negative/[4%] text-negative",
  icon: "grid size-[18px] place-items-center rounded-full border border-current",
};

const TONE_ICON = {
  info: { icon: <IconInfo className="size-2.5" />, color: "text-accent-blue" },
  success: { icon: <IconCheck className="size-2.5" />, color: "text-mint" },
  error: { icon: <IconAlert className="size-2.5" />, color: "" },
} as const;

interface NoticeProps {
  tone?: "info" | "success" | "error";
  children: ReactNode;
  className?: string;
}

/**
 * Notice is the inline alert banner (ported from UIKIT AdminNotice) —
 * stays in document flow, unlike a toast. Used for modal message areas
 * (SC-10 duplicate/failure) and form-level guidance.
 */
const Notice = ({ tone = "info", children, className }: NoticeProps) => {
  return (
    <div
      className={cn(
        styles.wrap,
        tone === "error" && styles.wrapError,
        className,
      )}
      role={tone === "error" ? "alert" : "status"}
    >
      <span
        aria-hidden="true"
        className={cn(styles.icon, TONE_ICON[tone].color)}
      >
        {TONE_ICON[tone].icon}
      </span>
      <div>{children}</div>
    </div>
  );
};

export default Notice;

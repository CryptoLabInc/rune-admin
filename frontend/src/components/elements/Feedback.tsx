import type { ReactNode } from "react";

import IconAlert from "@/components/icons/IconAlert";
import IconPlus from "@/components/icons/IconPlus";
import IconSpinner from "@/components/icons/IconSpinner";
import { cn } from "@/utils/cn";

const styles = {
  wrap: "bg-panel-solid/35 grid min-h-[92px] grid-cols-[32px_1fr_auto] items-center gap-3.5 rounded-lg border p-[18px]",
  icon: "border-mint/28 text-mint grid size-[30px] place-items-center rounded-full border",
  iconError: "border-negative/30 text-negative",
  title: "text-base",
  desc: "text-muted-foreground m-0 mt-1.5 text-sm leading-[1.45]",
};

interface FeedbackProps {
  state: "empty" | "loading" | "error";
  title: string;
  description?: string;
  action?: ReactNode;
  className?: string;
}

/**
 * Feedback is the empty/loading/error state panel (ported from UIKIT
 * AdminFeedback) — table empty views, fetch-failure screens (SC-06 C,
 * SC-11 C, SC-16 B), and the team-tree empty result. The optional
 * action slot holds a retry/create button.
 */
const Feedback = ({
  state,
  title,
  description,
  action,
  className,
}: FeedbackProps) => {
  return (
    <div
      className={cn(styles.wrap, className)}
      role={state === "error" ? "alert" : "status"}
    >
      {/* loading swaps the bordered circle for the spinner — its own
          ring is the track, so only the arc sweeps (a rotating border
          circle would drag the glyph around with it). */}
      {state === "loading" ? (
        <IconSpinner className="text-mint size-[30px]" />
      ) : (
        <span
          aria-hidden="true"
          className={cn(styles.icon, state === "error" && styles.iconError)}
        >
          {state === "empty" ? (
            <IconPlus className="size-3.5" />
          ) : (
            <IconAlert className="size-3.5" />
          )}
        </span>
      )}
      <div>
        <b className={styles.title}>{title}</b>
        {description && <p className={styles.desc}>{description}</p>}
      </div>
      {action && <div>{action}</div>}
    </div>
  );
};

export default Feedback;

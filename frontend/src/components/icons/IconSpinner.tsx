import { cn } from "@/utils/cn";

/**
 * IconSpinner is the loading spinner — a static ring track with a
 * rotating 100° arc (only the arc appears to sweep; nothing else
 * spins). Inherits color via currentColor; the track reuses the
 * ring color at 28% like the other Feedback state circles.
 */
const IconSpinner = ({ className }: { className?: string }) => (
  <svg
    viewBox="0 0 30 30"
    fill="none"
    className={cn(
      "animate-[spin_1.1s_linear_infinite] motion-reduce:animate-none",
      className,
    )}
    aria-hidden="true"
  >
    <circle
      cx="15"
      cy="15"
      r="13.5"
      stroke="currentColor"
      strokeOpacity={0.28}
      strokeWidth={1.5}
    />
    <circle
      cx="15"
      cy="15"
      r="13.5"
      stroke="currentColor"
      strokeWidth={1.5}
      strokeLinecap="round"
      pathLength={100}
      strokeDasharray="28 72"
    />
  </svg>
);

export default IconSpinner;

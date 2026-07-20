import { cn } from "@/utils/cn";
import { BTN_COLOR_VAR, BTN_SIZE_VAR } from "@/constants/styleConstants";
import type { TBTNColor, TBTNSize } from "@/types/styleTypes";

const styles = {
  /* focus-visible mirrors hover per theme (2026-07-13) — no separate
     offset outline ring; the theme's hot face is the focus indicator.
     Disabled is one shared flat gray fill for every theme — the
     :disabled pseudo-class outranks the per-theme classes, so it wins
     without opacity ghosting. */
  default:
    "relative inline-flex cursor-pointer items-center justify-center gap-2 border border-transparent font-semibold whitespace-nowrap transition-[background,color,box-shadow,border-color,transform] duration-[180ms] outline-none disabled:cursor-not-allowed disabled:border-transparent disabled:bg-muted-foreground/12 disabled:text-faint disabled:shadow-none",
};

interface ButtonProps {
  btnText: string;
  btnSize: TBTNSize;
  btnColor: TBTNColor;
  handleClick?:
    (() => void) | ((e: React.MouseEvent<HTMLButtonElement>) => void);
  disabled?: boolean;
  btnType?: "button" | "submit" | "reset";
  className?: string;
}

/**
 * Button is the shared action button (ported from UIKIT RuneButton).
 * Fills its container (w-full via size map). There is no loading UI —
 * while a triggered request is in flight, pass disabled (decided
 * 2026-07-13).
 */
const Button = ({
  btnText,
  btnSize,
  btnColor,
  handleClick = () => {},
  disabled = false,
  btnType = "button",
  className,
}: ButtonProps) => {
  return (
    <button
      type={btnType}
      className={cn(
        styles.default,
        BTN_SIZE_VAR[btnSize],
        BTN_COLOR_VAR[btnColor],
        className,
      )}
      onClick={handleClick}
      disabled={disabled}
    >
      {/* <span>{btnText}</span> */}
      {btnText}
    </button>
  );
};

export default Button;

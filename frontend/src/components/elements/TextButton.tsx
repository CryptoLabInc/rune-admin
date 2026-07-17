import { cn } from "@/utils/cn";
import { TEXT_BTN_TONE_VAR } from "@/constants/styleConstants";
import type { TTextBTNTone } from "@/types/styleTypes";

const styles = {
  default:
    "w-fit cursor-pointer p-1 text-sm underline underline-offset-4 transition-colors duration-[160ms] focus-visible:rounded-[4px] focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-mint disabled:cursor-not-allowed disabled:text-faint/50",
};

interface TextButtonProps {
  btnText: string;
  tone?: TTextBTNTone;
  disabled?: boolean;
  handleClick?: () => void;
  className?: string;
}

/**
 * TextButton is the quiet underlined action (ported from UIKIT
 * AdminTextButton) — for low-emphasis actions like 회원탈퇴 or row-level
 * 삭제 (red tone).
 */
const TextButton = ({
  btnText,
  tone = "gray",
  disabled = false,
  handleClick = () => {},
  className,
}: TextButtonProps) => {
  return (
    <button
      type="button"
      className={cn(styles.default, TEXT_BTN_TONE_VAR[tone], className)}
      onClick={handleClick}
      disabled={disabled}
    >
      {btnText}
    </button>
  );
};

export default TextButton;

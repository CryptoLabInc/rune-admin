import { cn } from "@/utils/cn";

const styles = {
  wrap: "inline-flex w-fit cursor-pointer items-center gap-2 text-base text-foreground",
  wrapDisabled: "cursor-not-allowed opacity-40",
  box: "grid size-4 place-items-center rounded-[4px] border border-border-strong text-xs text-on-mint peer-checked:border-mint peer-checked:bg-mint peer-checked:shadow-[inset_0_0_0_1px_rgba(4,36,31,0.18)] peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2 peer-focus-visible:outline-mint",
  label: "font-medium",
};

interface CheckboxProps {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label?: string;
  disabled?: boolean;
  ariaLabel?: string;
  className?: string;
}

/**
 * Checkbox is the mint check control (ported from UIKIT AdminCheckbox).
 * A select-all checkbox only toggles the other rows through its
 * onChange handler — it carries no separate visual state of its own.
 */
const Checkbox = ({
  checked,
  onChange,
  label,
  disabled = false,
  ariaLabel,
  className,
}: CheckboxProps) => {
  return (
    <label
      className={cn(styles.wrap, disabled && styles.wrapDisabled, className)}
    >
      <input
        type="checkbox"
        className="peer sr-only"
        checked={checked}
        disabled={disabled}
        aria-label={ariaLabel}
        onChange={(e) => onChange(e.target.checked)}
      />
      {/* The glyph always renders (transparent when unchecked) so the
          baseline never shifts — a toggling glyph changes the line box
          and jiggles table-row heights. */}
      <span
        aria-hidden="true"
        className={cn(styles.box, !checked && "text-transparent")}
      >
        ✓
      </span>
      {label && <span className={styles.label}>{label}</span>}
    </label>
  );
};

export default Checkbox;

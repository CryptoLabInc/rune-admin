import { cn } from "@/utils/cn";

const styles = {
  wrap: "flex w-full flex-col gap-1",
  label: "pb-1 text-sm font-semibold text-foreground",
  /* Focus is border-color only — no ring shadow (decided 2026-07-13). */
  input:
    "h-9 w-full appearance-none rounded-md border border-border-strong bg-well/50 px-3 text-base text-foreground transition-[border-color] duration-[160ms] outline-none placeholder:text-faint focus:border-mint/55 disabled:cursor-not-allowed disabled:opacity-50",
  invalid: "border-negative/60 focus:border-negative/70",
  hint: "text-xs leading-normal text-faint",
  error: "text-xs leading-normal text-negative",
};

interface InputProps {
  id: string;
  labelText: string;
  type?: "text" | "password" | "email";
  placeholder?: string;
  maxLength?: number;
  disabled?: boolean;
  hint?: string;
  error?: string;
  value: string;
  setValue: (value: string) => void;
  /** Blur-time validation hook (e.g. email format warning, SC-12 no.1). */
  onBlur?: () => void;
  className?: string;
}

/**
 * Input is the labeled form field (ported from UIKIT RuneField):
 * label + input + one hint/error line. An error replaces the hint and
 * is announced via role="alert". Focus lights the mint ring (CSS only).
 */
const Input = ({
  id,
  labelText,
  type = "text",
  placeholder,
  maxLength = 100,
  disabled = false,
  hint = "",
  error = "",
  value,
  setValue,
  onBlur,
  className,
}: InputProps) => {
  return (
    <div className={cn(styles.wrap, className)}>
      <label className={styles.label} htmlFor={id}>
        {labelText}
      </label>
      <input
        id={id}
        name={id}
        type={type}
        className={cn(styles.input, !!error && styles.invalid)}
        placeholder={placeholder}
        maxLength={maxLength}
        disabled={disabled}
        value={value}
        aria-invalid={!!error || undefined}
        aria-describedby={hint || error ? `${id}-desc` : undefined}
        onChange={(e) => setValue(e.target.value)}
        onBlur={onBlur}
      />
      {error ? (
        <p role="alert" id={`${id}-desc`} className={styles.error}>
          {error}
        </p>
      ) : hint ? (
        <p id={`${id}-desc`} className={styles.hint}>
          {hint}
        </p>
      ) : null}
    </div>
  );
};

export default Input;

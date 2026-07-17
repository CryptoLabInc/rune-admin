import { cn } from "@/utils/cn";

const styles = {
  wrap: "relative grid w-[min(280px,100%)] cursor-pointer grid-cols-[18px_1fr] gap-2 rounded-md border border-border bg-muted-foreground/[2%] px-3 py-3 text-muted-foreground",
  wrapActive: "border-mint/40 bg-mint/[4%] text-foreground",
  dot: "mt-0 size-4 rounded-full border border-border-strong",
  dotActive: "border-4 border-mint bg-background",
  label: "block text-base font-semibold",
  desc: "mt-1 block text-xs leading-relaxed text-faint",
};

interface RadioProps {
  checked: boolean;
  onChange: () => void;
  name: string;
  label: string;
  desc?: string;
  className?: string;
}

/**
 * Radio is the card-style radio option (ported from UIKIT AdminRadio):
 * a bordered card with a dot, label, and optional description line.
 * The unselected card keeps a slightly raised background (wireframe
 * SC-09 rule). Width follows the UIKIT card: min(280px, 100%).
 */
const Radio = ({
  checked,
  onChange,
  name,
  label,
  desc,
  className,
}: RadioProps) => {
  return (
    <label className={cn(styles.wrap, checked && styles.wrapActive, className)}>
      <input
        type="radio"
        className="peer sr-only"
        name={name}
        checked={checked}
        onChange={onChange}
      />
      <span
        aria-hidden="true"
        className={cn(
          styles.dot,
          checked && styles.dotActive,
          "peer-focus-visible:outline-mint peer-focus-visible:outline-2 peer-focus-visible:outline-offset-2",
        )}
      />
      <span>
        <span className={styles.label}>{label}</span>
        {desc && <span className={styles.desc}>{desc}</span>}
      </span>
    </label>
  );
};

export default Radio;

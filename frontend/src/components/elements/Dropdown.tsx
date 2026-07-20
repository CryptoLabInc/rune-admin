import { useCallback, useEffect, useId, useRef, useState } from "react";
import type { CSSProperties, KeyboardEvent as ReactKeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { ChevronDown } from "lucide-react";

import { cn } from "@/utils/cn";
import type { TDropdownOption } from "@/types/commonTypes";

const styles = {
  field: "grid w-full min-w-0 gap-2",
  label: "text-sm font-semibold text-foreground",
  trigger:
    "flex w-full cursor-pointer items-center justify-between gap-2 border border-border-strong bg-well/70 px-2 text-left text-foreground transition-[border-color,box-shadow] duration-[160ms] outline-none focus-visible:border-mint/60 focus-visible:shadow-[0_0_0_3px] focus-visible:shadow-mint/10 disabled:cursor-not-allowed disabled:opacity-45",
  /* Radius follows the height tier: 36px controls round 10px, 32px
     controls round 8px (Button sm precedent). */
  triggerSize: {
    md: "h-9 rounded-md text-base",
    sm: "h-8 rounded-[8px] text-sm",
  },
  triggerOpen: "border-mint/60 shadow-[0_0_0_3px] shadow-mint/10",
  /* Unsaved (staged) value — accent-blue border, the color reserved for
     the role-change pending state (2차-A rule / D24). A 1px ring shadow
     thickens the line to ~2px without shifting layout (border-2 would).
     The mint open ring still wins while interacting (listed after this
     in cn). */
  triggerChanged: "border-accent-blue shadow-[0_0_0_1px] shadow-accent-blue/60",
  /* Error keeps the red border through focus/open, and re-tones the
     ring shadow to red — a mint ring on a red border reads as a clash.
     The bare shadow-negative/10 only shows once triggerOpen (listed
     before this in cn) supplies the ring size. */
  triggerError:
    "border-negative/60 shadow-negative/10 focus-visible:border-negative/70 focus-visible:shadow-negative/10",
  valueText: "overflow-hidden text-ellipsis whitespace-nowrap",
  placeholder: "text-faint",
  /* The chevron keeps its default color in every state (open included,
     decided 2026-07-13) — only the rotation animates. */
  chevron: "flex-none text-faint transition-transform duration-300 ease-sweep",
  chevronOpen: "rotate-180",
  /* Scrollbar matches the app-wide thin mint treatment (Table scroll
     area) instead of the default browser bar. */
  menu: "fixed z-80 m-0 max-h-[288px] list-none overflow-y-auto overscroll-contain rounded-md border border-border-strong bg-panel-solid p-1 text-foreground shadow-[0_24px_48px_-20px_rgba(0,0,0,0.65)] animate-menu-pop motion-reduce:animate-none [scrollbar-width:thin] [scrollbar-color:color-mix(in_srgb,var(--color-mint)_32%,transparent)_color-mix(in_srgb,var(--color-muted-foreground)_8%,transparent)]",
  menuSize: {
    md: "text-base",
    sm: "text-sm",
  },
  menuUp: "origin-bottom animate-menu-pop-up",
  option:
    "flex cursor-pointer items-center gap-2 rounded-sm py-2 pr-2 leading-tight whitespace-nowrap text-muted-foreground animate-menu-item motion-reduce:animate-none",
  optionHi: "bg-mint/8 text-foreground",
  optionSelected: "text-mint",
  optionDisabled: "cursor-not-allowed opacity-40",
  check: "w-3 flex-none text-xs text-mint",
  optionLabel: "overflow-hidden text-ellipsis",
  hint: "text-xs leading-relaxed text-faint",
  error: "text-xs leading-relaxed text-negative",
};

type MenuPos = {
  left: number;
  width: number;
  top?: number;
  bottom?: number;
  up: boolean;
};

interface DropdownProps {
  options: TDropdownOption[];
  label?: string;
  placeholder?: string;
  hint?: string;
  error?: string;
  value?: string;
  defaultValue?: string;
  onChange?: (value: string) => void;
  disabled?: boolean;
  /** md 36px/14px text (form default) · sm 32px/12px text (toolbars, table cells) */
  size?: "sm" | "md";
  /** Staged-but-unsaved value — accent-blue trigger border (D24). */
  changed?: boolean;
  ariaLabel?: string;
  className?: string;
}

/**
 * Dropdown is the custom listbox select (ported from UIKIT AdminSelect).
 * Native <select> is forbidden: the OS option popup cannot be themed.
 * The menu renders through a portal with fixed positioning so it never
 * clips inside scroll areas/dialogs, and flips upward when there is no
 * room below. Keyboard: ArrowUp/Down, Enter/Space, Escape, Tab.
 */
const Dropdown = ({
  options,
  label,
  placeholder = "선택",
  hint,
  error,
  value,
  defaultValue,
  onChange,
  disabled = false,
  size = "md",
  changed = false,
  ariaLabel,
  className,
}: DropdownProps) => {
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<MenuPos | null>(null);
  const [hi, setHi] = useState(-1);
  const [inner, setInner] = useState(defaultValue ?? "");
  const current = value !== undefined ? value : inner;
  const rootRef = useRef<HTMLLabelElement>(null);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLUListElement>(null);
  const id = useId();
  const selectedIndex =
    current === "" ? -1 : options.findIndex((o) => o.value === current);
  const selected = selectedIndex >= 0 ? options[selectedIndex] : undefined;

  const close = useCallback(() => {
    setOpen(false);
    setPos(null);
  }, []);

  const openMenu = () => {
    if (disabled || options.length === 0) return;
    const rect = triggerRef.current!.getBoundingClientRect();
    const menuHeight = Math.min(288, options.length * 36 + 12);
    const up =
      rect.bottom + 6 + menuHeight > window.innerHeight &&
      rect.top - 6 - menuHeight > 0;
    setPos({
      left: rect.left,
      width: rect.width,
      up,
      top: up ? undefined : rect.bottom + 6,
      bottom: up ? window.innerHeight - rect.top + 6 : undefined,
    });
    const firstEnabled = options.findIndex((o) => !o.disabled);
    setHi(selectedIndex >= 0 ? selectedIndex : firstEnabled);
    setOpen(true);
  };

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: PointerEvent) => {
      const target = event.target as Node;
      if (
        !rootRef.current?.contains(target) &&
        !menuRef.current?.contains(target)
      )
        close();
    };
    const onScroll = (event: Event) => {
      if (
        event.target instanceof Node &&
        menuRef.current?.contains(event.target)
      )
        return;
      close();
    };
    document.addEventListener("pointerdown", onPointerDown);
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", close);
    return () => {
      document.removeEventListener("pointerdown", onPointerDown);
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", close);
    };
  }, [open, close]);

  const pick = (option: TDropdownOption) => {
    if (option.disabled) return;
    if (value === undefined) setInner(option.value);
    onChange?.(option.value);
    close();
    triggerRef.current?.focus();
  };

  const move = (dir: 1 | -1) => {
    if (options.length === 0) return;
    let next = hi;
    for (let i = 0; i < options.length; i += 1) {
      next = (next + dir + options.length) % options.length;
      if (!options[next].disabled) break;
    }
    setHi(next);
    menuRef.current?.children[next]?.scrollIntoView({ block: "nearest" });
  };

  const onKeyDown = (event: ReactKeyboardEvent<HTMLButtonElement>) => {
    if (!open) {
      if (["ArrowDown", "ArrowUp", "Enter", " "].includes(event.key)) {
        event.preventDefault();
        openMenu();
      }
      return;
    }
    if (event.key === "ArrowDown") {
      event.preventDefault();
      move(1);
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      move(-1);
    } else if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      if (hi >= 0) pick(options[hi]);
    } else if (event.key === "Escape") {
      event.preventDefault();
      close();
    } else if (event.key === "Tab") {
      close();
    }
  };

  const menu =
    open &&
    pos &&
    createPortal(
      <ul
        ref={menuRef}
        id={`${id}-menu`}
        role="listbox"
        aria-labelledby={label ? `${id}-label` : undefined}
        aria-label={label ? undefined : (ariaLabel ?? placeholder)}
        className={cn(
          styles.menu,
          styles.menuSize[size],
          pos.up && styles.menuUp,
        )}
        style={{
          left: pos.left,
          width: pos.width,
          top: pos.top,
          bottom: pos.bottom,
        }}
      >
        {options.map((option, index) => (
          <li
            key={option.value}
            id={`${id}-opt-${index}`}
            role="option"
            aria-selected={option.value === current}
            aria-disabled={option.disabled || undefined}
            className={cn(
              styles.option,
              index === hi && styles.optionHi,
              option.value === current && styles.optionSelected,
              option.disabled && styles.optionDisabled,
            )}
            style={
              {
                paddingLeft: 10 + (option.depth ?? 0) * 14,
                animationDelay: `${Math.min(index, 8) * 14}ms`,
              } as CSSProperties
            }
            onPointerMove={() => !option.disabled && setHi(index)}
            onClick={() => pick(option)}
          >
            <span aria-hidden="true" className={styles.check}>
              {option.value === current ? "✓" : ""}
            </span>
            <span className={styles.optionLabel}>{option.label}</span>
          </li>
        ))}
      </ul>,
      document.body,
    );

  return (
    <label ref={rootRef} className={cn(styles.field, className)} htmlFor={id}>
      {label && (
        <span className={styles.label} id={`${id}-label`}>
          {label}
        </span>
      )}
      <span className="relative block">
        <button
          ref={triggerRef}
          type="button"
          id={id}
          className={cn(
            styles.trigger,
            styles.triggerSize[size],
            changed && styles.triggerChanged,
            open && styles.triggerOpen,
            !!error && styles.triggerError,
          )}
          aria-haspopup="listbox"
          aria-expanded={open}
          aria-controls={open ? `${id}-menu` : undefined}
          aria-activedescendant={
            open && hi >= 0 ? `${id}-opt-${hi}` : undefined
          }
          aria-invalid={!!error || undefined}
          aria-label={label ? undefined : (ariaLabel ?? placeholder)}
          aria-describedby={hint || error ? `${id}-desc` : undefined}
          disabled={disabled}
          onClick={() => (open ? close() : openMenu())}
          onKeyDown={onKeyDown}
        >
          <span
            className={cn(styles.valueText, !selected && styles.placeholder)}
          >
            {selected?.label ?? placeholder}
          </span>
          <ChevronDown
            aria-hidden="true"
            size={16}
            className={cn(styles.chevron, open && styles.chevronOpen)}
          />
        </button>
      </span>
      {error ? (
        <span id={`${id}-desc`} className={styles.error}>
          {error}
        </span>
      ) : hint ? (
        <span id={`${id}-desc`} className={styles.hint}>
          {hint}
        </span>
      ) : null}
      {menu}
    </label>
  );
};

export default Dropdown;

import { Search } from "lucide-react";

import { cn } from "@/utils/cn";
import { L } from "@/locales";

const styles = {
  wrap: "relative inline-flex h-8 w-full items-center rounded-[8px] border border-border-strong bg-well/70 transition-[border-color,box-shadow] duration-[160ms] focus-within:border-mint/60 focus-within:shadow-[0_0_0_3px] focus-within:shadow-mint/10",
  icon: "flex flex-none items-center pl-2 text-faint",
  input:
    "h-full min-w-0 flex-1 border-0 bg-transparent pr-2 pl-2 text-base text-foreground outline-none placeholder:text-faint disabled:cursor-not-allowed [&::-webkit-search-cancel-button]:hidden",
  clear:
    "mr-1 cursor-pointer rounded-sm px-2 py-1 text-xs text-faint hover:bg-muted-foreground/10 hover:text-foreground",
};

interface SearchInputProps {
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  maxLength?: number;
  disabled?: boolean;
  ariaLabel?: string;
  className?: string;
}

/**
 * SearchInput is the search field with a ⌕ icon and a clear (✕) button
 * (ported from UIKIT AdminSearch). Used for 팀 검색 (max 50) and 계정
 * 검색 (max 100).
 */
const SearchInput = ({
  value,
  onChange,
  placeholder = L.elements.search,
  maxLength,
  disabled = false,
  ariaLabel,
  className,
}: SearchInputProps) => {
  return (
    <span
      className={cn(
        styles.wrap,
        disabled && "cursor-not-allowed opacity-45",
        className,
      )}
    >
      <span aria-hidden="true" className={styles.icon}>
        <Search size={16} />
      </span>
      <input
        type="search"
        className={styles.input}
        value={value}
        placeholder={placeholder}
        maxLength={maxLength}
        disabled={disabled}
        aria-label={ariaLabel ?? placeholder}
        onChange={(e) => onChange(e.target.value)}
      />
      {value !== "" && !disabled && (
        <button
          type="button"
          className={styles.clear}
          aria-label={L.elements.clear}
          onClick={() => onChange("")}
        >
          ✕
        </button>
      )}
    </span>
  );
};

export default SearchInput;

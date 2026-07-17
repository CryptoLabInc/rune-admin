import { cn } from "@/utils/cn";

const styles = {
  wrap: "flex w-fit items-center gap-2 font-mono text-sm",
  /* Arrows use the control-grade border + ink (border-strong /
     foreground — same grammar as Input/Dropdown controls). */
  arrow:
    "border-border-strong grid h-6 min-w-6 cursor-pointer place-items-center rounded-[4px] border px-1 text-foreground transition-colors duration-[160ms] hover:enabled:border-mint/40 hover:enabled:text-mint disabled:cursor-not-allowed disabled:opacity-30",
  num: "grid h-6 min-w-6 cursor-pointer place-items-center rounded-[4px] px-1 text-muted-foreground transition-colors duration-[160ms] hover:text-mint",
  numActive: "bg-mint text-on-mint font-semibold hover:text-on-mint",
};

interface PaginationProps {
  page: number;
  totalPages: number;
  onChange: (page: number) => void;
  className?: string;
}

/**
 * Pagination is the numbered pager (wireframe spec form): ‹ 1 2 3 ›.
 * The current page is filled mint; boundary arrows disable. Page-count
 * ellipsis (…) is deferred until a screen needs it.
 */
const Pagination = ({
  page,
  totalPages,
  onChange,
  className,
}: PaginationProps) => {
  return (
    <nav className={cn(styles.wrap, className)} aria-label="페이지네이션">
      <button
        type="button"
        className={styles.arrow}
        aria-label="이전 페이지"
        disabled={page <= 1}
        onClick={() => onChange(page - 1)}
      >
        ‹
      </button>
      {Array.from({ length: totalPages }, (_, i) => i + 1).map((n) => (
        <button
          key={n}
          type="button"
          className={cn(styles.num, n === page && styles.numActive)}
          aria-current={n === page ? "page" : undefined}
          onClick={() => n !== page && onChange(n)}
        >
          {n}
        </button>
      ))}
      <button
        type="button"
        className={styles.arrow}
        aria-label="다음 페이지"
        disabled={page >= totalPages}
        onClick={() => onChange(page + 1)}
      >
        ›
      </button>
    </nav>
  );
};

export default Pagination;

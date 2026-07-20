import { useEffect, useState } from "react";

/**
 * useDebouncedValue returns `value` delayed by `delayMs` — it only updates
 * after the value has stopped changing for that long. Used to keep the user
 * search from re-querying on every keystroke.
 */
export const useDebouncedValue = <T>(value: T, delayMs: number): T => {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const id = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(id);
  }, [value, delayMs]);
  return debounced;
};

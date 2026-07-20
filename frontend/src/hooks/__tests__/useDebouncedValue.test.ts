import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useDebouncedValue } from "@/hooks/useDebouncedValue";

describe("useDebouncedValue", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("returns the previous value until the delay elapses", () => {
    const { result, rerender } = renderHook(
      ({ v }) => useDebouncedValue(v, 300),
      { initialProps: { v: "a" } },
    );
    expect(result.current).toBe("a");
    rerender({ v: "ab" });
    expect(result.current).toBe("a"); // not yet
    act(() => {
      vi.advanceTimersByTime(300);
    });
    expect(result.current).toBe("ab");
  });
});

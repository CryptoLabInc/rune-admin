import { describe, expect, it } from "vitest";

import { formatDate, formatDateTime } from "@/utils/formatDate";

describe("formatDate (KST)", () => {
  it("renders the UTC instant in KST (UTC+9)", () => {
    expect(formatDate("2026-07-07T08:12:00Z")).toBe("2026-07-07");
    expect(formatDateTime("2026-07-07T08:12:00Z")).toBe("2026-07-07 17:12");
  });

  it("rolls the date forward when +9h crosses midnight", () => {
    // 15:00Z → 00:00 KST the next day.
    expect(formatDate("2026-07-07T15:00:00Z")).toBe("2026-07-08");
    expect(formatDateTime("2026-07-07T15:00:00Z")).toBe("2026-07-08 00:00");
  });

  it("keeps a 24h clock (no 24:00 at KST midnight)", () => {
    expect(formatDateTime("2026-01-01T15:00:00Z")).toBe("2026-01-02 00:00");
  });

  it("renders — for null/undefined", () => {
    expect(formatDate(null)).toBe("—");
    expect(formatDateTime(null)).toBe("—");
    expect(formatDate(undefined)).toBe("—");
    expect(formatDateTime(undefined)).toBe("—");
  });
});

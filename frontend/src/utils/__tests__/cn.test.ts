import { describe, expect, it } from "vitest";

import { cn } from "@/utils/cn";

describe("cn", () => {
  it("keeps custom font-size tokens alongside text color classes", () => {
    expect(cn("text-tag text-mint")).toBe("text-tag text-mint");
    expect(cn("text-md", "text-negative")).toBe("text-md text-negative");
  });

  it("still resolves real conflicts to the last class", () => {
    expect(cn("text-tag", "text-sm")).toBe("text-sm");
    expect(cn("text-mint", "text-negative")).toBe("text-negative");
  });
});

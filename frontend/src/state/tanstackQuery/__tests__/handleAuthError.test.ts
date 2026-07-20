import { beforeEach, describe, expect, it, vi } from "vitest";

import { PATH_LIST } from "@/constants/commonConstants";

vi.mock("@/utils/redirect", () => ({ redirectTo: vi.fn() }));

// The module keeps a `redirecting` flag at module scope. Re-import it fresh per
// test (via resetModules) so each case starts with redirecting === false, and
// grab the mocked redirectTo from the same fresh registry to assert on.
const load = async () => {
  const mod = await import("@/state/tanstackQuery/handleAuthError");
  const { redirectTo } = await import("@/utils/redirect");
  return { ...mod, redirectTo: vi.mocked(redirectTo) };
};

describe("handleAuthError", () => {
  beforeEach(() => {
    // resetModules gives a fresh `redirecting` flag; clearAllMocks resets the
    // redirectTo call history, which survives resetModules on its own.
    vi.resetModules();
    vi.clearAllMocks();
    window.history.pushState({}, "", PATH_LIST.teams);
  });

  const res = (status: number) => new Response(null, { status });

  it("redirects to login once on a 401 Response", async () => {
    const { handleAuthError, redirectTo } = await load();
    handleAuthError(res(401));
    expect(redirectTo).toHaveBeenCalledExactlyOnceWith(PATH_LIST.login);
  });

  it("ignores non-401 responses and non-Response errors", async () => {
    const { handleAuthError, redirectTo } = await load();
    handleAuthError(res(403));
    handleAuthError(res(500));
    handleAuthError(new Error("network"));
    expect(redirectTo).not.toHaveBeenCalled();
  });

  it("does not redirect when already on /login", async () => {
    window.history.pushState({}, "", PATH_LIST.login);
    const { handleAuthError, redirectTo } = await load();
    handleAuthError(res(401));
    expect(redirectTo).not.toHaveBeenCalled();
  });

  it("collapses a burst of 401s into a single redirect", async () => {
    const { handleAuthError, redirectTo } = await load();
    handleAuthError(res(401));
    handleAuthError(res(401));
    handleAuthError(res(401));
    expect(redirectTo).toHaveBeenCalledOnce();
  });
});

describe("isUnauthorized", () => {
  beforeEach(() => {
    vi.resetModules();
    vi.clearAllMocks();
  });

  it("is true only for a 401 Response", async () => {
    const { isUnauthorized } = await load();
    expect(isUnauthorized(new Response(null, { status: 401 }))).toBe(true);
    expect(isUnauthorized(new Response(null, { status: 403 }))).toBe(false);
    expect(isUnauthorized(new Response(null, { status: 200 }))).toBe(false);
    expect(isUnauthorized(new Error("nope"))).toBe(false);
    expect(isUnauthorized(null)).toBe(false);
  });
});

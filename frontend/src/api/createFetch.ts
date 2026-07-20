const API_BASE_URL = import.meta.env.VITE_API_BASE_URL ?? "/api/v1";

/**
 * serviceFetch calls the rune-console console backend and returns the raw
 * Response; callers throw the Response itself on !res.ok. Token refresh
 * will be layered in here once the console auth flow (O3/O4) is settled.
 */
export const serviceFetch = async (urlPath: string, options?: RequestInit) =>
  await fetch(`${API_BASE_URL}/${urlPath}`, {
    credentials: "include",
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...options?.headers,
    },
  });

/**
 * consoleFetch calls the auth/session endpoints that live OUTSIDE the /api/v1
 * base path (e.g. /console/auth/start, /console/session, /auth/*). Same
 * credentialed JSON defaults as serviceFetch; pass a root-relative path
 * without a leading slash.
 */
export const consoleFetch = async (urlPath: string, options?: RequestInit) =>
  await fetch(`/${urlPath}`, {
    credentials: "include",
    ...options,
    headers: {
      "Content-Type": "application/json",
      ...options?.headers,
    },
  });

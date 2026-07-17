import { redirectTo } from "@/utils/redirect";
import { PATH_LIST } from "@/constants/commonConstants";

/**
 * isUnauthorized narrows an unknown react-query error to a 401 Response.
 * createFetch callers throw the raw Response on !res.ok, so the error that
 * reaches the cache handlers is the Response itself.
 */
export const isUnauthorized = (error: unknown): boolean =>
  error instanceof Response && error.status === 401;

// A single expiry fans out to a 401 on every in-flight query at once; only the
// first should drive the redirect. Never reset — the hard redirect unloads the
// page and re-initializes this module.
let redirecting = false;

/**
 * handleAuthError sends the browser to the sign-in screen on a 401 (session
 * expired mid-use). Non-401 errors, and errors raised while already on /login,
 * are ignored. It uses a full-page redirect so the reload clears all
 * react-query cache and rendered data (login-flow wireframe LG-04: "clear
 * displayed data then redirect"), which is why no manual queryClient.clear()
 * is needed. 403/5xx/network errors fall through to each view's own error UI.
 */
export const handleAuthError = (error: unknown): void => {
  if (!isUnauthorized(error)) return;
  if (window.location.pathname === PATH_LIST.login) return;
  if (redirecting) return;
  redirecting = true;
  redirectTo(PATH_LIST.login);
};

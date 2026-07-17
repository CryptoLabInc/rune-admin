/**
 * TSessionMe is the signed-in admin identity from GET /console/session. The
 * backend returns the full cloud principal (id, email, name, picture, …) with
 * an `avatar` mirror of `picture`, so this is a superset — the console reads
 * only `email` and `avatar`. `email` is always present (login is refused
 * without it); `avatar` is present only when the principal carries a picture,
 * otherwise the UI falls back to a default glyph.
 */
export type TSessionMe = {
  email: string;
  avatar?: string;
  [key: string]: unknown;
};

/**
 * TSession is the /console/session response — the single truth for the route
 * guard. It always resolves 200; the discriminant is `logged_in`.
 *
 * `plan` is the account's subscription plan as a lowercase wire string
 * (currently always "free"; the value set is open). It is a console-owned,
 * top-level field — separate from `me` (the cloud principal passthrough) — so
 * its source can change without reshaping the principal. The UI capitalizes it
 * for display and falls back to "Free" when empty.
 */
export type TSession =
  | { logged_in: false }
  | { logged_in: true; expires_at: string; plan: string; me: TSessionMe };

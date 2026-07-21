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
 * The console is a single-admin surface: the first account to sign in claims it
 * as the owner. `is_owner` is false when a DIFFERENT account is signed in (soft
 * block) — it reaches the app but cannot use it, and `owner_email` names the
 * owner so the UI can say whom to ask. Both are optional for forward-compat with
 * an older backend that omits them (treated as owner — no gate).
 */
export type TSession =
  | { logged_in: false }
  | {
      logged_in: true;
      expires_at: string;
      me: TSessionMe;
      is_owner?: boolean;
      owner_email?: string;
    };

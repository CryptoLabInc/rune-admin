// Runtime configuration, all overridable via environment variables.
const num = (name: string, fallback: number): number => {
  const raw = process.env[name];
  if (raw === undefined || raw.trim() === "") return fallback;
  const n = Number(raw);
  return Number.isFinite(n) ? n : fallback;
};

const bool = (name: string, fallback: boolean): boolean => {
  const raw = process.env[name];
  if (raw === undefined) return fallback;
  return raw === "1" || raw.toLowerCase() === "true";
};

const str = (name: string, fallback: string): string => {
  const raw = process.env[name];
  return raw === undefined || raw.trim() === "" ? fallback : raw;
};

export const config = {
  /** Port the mock listens on. */
  port: num("MOCK_PORT", 4000),
  /**
   * Frontend origin the OAuth callback redirects back to. Defaults to the
   * Vite dev server. Set to the mock's own origin to drive the flow directly.
   */
  appOrigin: str("MOCK_APP_ORIGIN", "http://localhost:5173"),
  /** Console session lifetime, in ms (default 30 min). */
  sessionTtlMs: num("MOCK_SESSION_TTL_MS", 30 * 60 * 1000),
  /** Whether the server boots with an established session (dev convenience). */
  startLoggedIn: bool("MOCK_START_LOGGED_IN", true),
  /** Sliding expiry: extend the session on each authenticated API call. */
  sliding: bool("MOCK_SLIDING", false),
  /** Delay before a workspace phase transition completes, in ms. */
  phaseDelayMs: num("MOCK_PHASE_DELAY_MS", 2500),
  /**
   * Enforce the Sec-Fetch-Site origin guard (403 ORIGIN_FORBIDDEN on
   * cross-site). Off by default so curl/tests are never blocked.
   */
  originGuard: bool("MOCK_ORIGIN_GUARD", false),
} as const;

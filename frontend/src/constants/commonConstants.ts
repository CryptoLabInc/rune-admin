import { L } from "@/locales";

/** BRAND_WORDMARK is the displayed product wordmark, shared by the navbars
 * (SC-01/SC-03) and the login card heading (SC-01) so the brand string has a
 * single source of truth. Deliberately not localized. */
export const BRAND_WORDMARK = "RUNE CONSOLE";

/** WORKSPACE_MAX_MEMORIES is the plan cap on stored memories (rows) per
 * workspace; the SC-02 modal renders usage as rowCount / max (percent). */
export const WORKSPACE_MAX_MEMORIES = 1000;

/** BTN_TEXT is the single source of truth for visible action-button labels
 * (Button `btnText` / TextButton) across the console screens — the wording
 * itself lives in src/locales (en.ts / ko.ts, chosen once at page load).
 * Icon-button aria-labels are intentionally out of scope — those are
 * accessibility strings, not button captions. */
export const BTN_TEXT = L.btn;

/** MODAL_TITLES is the single source of truth for ModalLayout titles across
 * the console modals, mirroring BTN_TEXT (wording in src/locales). Titles
 * that embed a name or count are functions; the rest are plain strings. */
export const MODAL_TITLES = L.modal;

export const PATH_LIST = {
  home: "/",
  login: "/login",
  workspace: "/workspace",
  teams: "/teams",
  users: "/users",
  sessions: "/sessions",
  uiTest: "/ui-test",
} as const;

export const NAV_LIST = [
  { title: L.nav.teams, url: PATH_LIST.teams },
  { title: L.nav.users, url: PATH_LIST.users },
  { title: L.nav.sessions, url: PATH_LIST.sessions },
] as const;

export const QUERY_KEYS = {
  teamsTree: "teamsTree",
  users: "users",
  usersStats: "usersStats",
  session: "session",
  team: "team",
  teamMembers: "teamMembers",
  workspace: "workspace",
  user: "user",
  invitations: "invitations",
  systemUpdate: "systemUpdate",
} as const;

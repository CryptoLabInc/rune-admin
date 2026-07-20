// In-memory state for the mock server: seed data, session, and simulated
// async workspace phase transitions. State resets whenever the process
// restarts (or via POST /__mock/reset).
import { config } from "./config.ts";
import { HttpError, isoIn } from "./http.ts";
import type {
  InvitationRow,
  Membership,
  Session,
  Team,
  User,
  Workspace,
  WorkspacePhase,
} from "./types.ts";

export type State = {
  teams: Team[];
  memberships: Membership[];
  users: User[];
  invitations: InvitationRow[];
  workspace: Workspace;
  // Dev-only one-shot failure arming for workspace ops (op → error to throw),
  // set via POST /__mock/workspace/fail so the frontend can reproduce the
  // SC-02 failure screens (D-1 실패 / D-2 / D-3 / D-4).
  workspaceFail: Map<string, { status: number; code: string }>;
  session: Session;
  // Pending OAuth `state` values issued by /console/auth/start, consumed once
  // by /auth/callback.
  pendingAuthStates: Set<string>;
  seq: number;
};

const ADMIN: Session["me"] = {
  email: "admin@corp.com",
  avatar: "https://i.pravatar.cc/128?u=admin@corp.com",
};

const daysAgo = (n: number): string => isoIn(-n * 24 * 60 * 60 * 1000);

const seedTeams = (): Team[] => [
  { id: "t_1", name: "Platform", parentId: null, createdAt: daysAgo(40) },
  { id: "t_2", name: "Payments", parentId: "t_1", createdAt: daysAgo(30) },
  { id: "t_3", name: "Infra", parentId: "t_1", createdAt: daysAgo(28) },
  { id: "t_4", name: "Data", parentId: null, createdAt: daysAgo(20) },
  { id: "t_5", name: "Growth", parentId: "t_4", createdAt: daysAgo(10) },
];

type Seed = {
  account: string;
  username: string;
  invitationStatus: User["invitationStatus"];
  sessionStatus: User["sessionStatus"];
  teams: Array<[string, Membership["role"]]>;
};

// 14 users with varied statuses + memberships so that pagination (size 10),
// status filters, and team filters all have something to show. Usernames mix
// hangul and lowercase-latin forms (the two alphabets the rule allows).
const USER_SEED: Seed[] = [
  { account: "kim@corp.com", username: "김철수", invitationStatus: "invite_redeemed", sessionStatus: "online", teams: [["t_1", "edit"], ["t_2", "write"]] },
  { account: "lee@corp.com", username: "lee young hee", invitationStatus: "invite_redeemed", sessionStatus: "online", teams: [["t_1", "read"]] },
  { account: "park@corp.com", username: "박민준", invitationStatus: "invite_pending", sessionStatus: "offline", teams: [["t_2", "read"]] },
  { account: "choi@corp.com", username: "최지우", invitationStatus: "invite_redeemed", sessionStatus: "online", teams: [["t_3", "edit"]] },
  { account: "jung@corp.com", username: "정다은", invitationStatus: "invite_redeemed", sessionStatus: "offline", teams: [["t_1", "write"], ["t_3", "read"]] },
  { account: "kang@corp.com", username: "강호진", invitationStatus: "invite_expired", sessionStatus: "offline", teams: [["t_4", "read"]] },
  { account: "cho@corp.com", username: "cho min soo", invitationStatus: "invite_redeemed", sessionStatus: "online", teams: [["t_4", "edit"], ["t_5", "edit"]] },
  { account: "yoon@corp.com", username: "윤아름", invitationStatus: "invite_pending", sessionStatus: "offline", teams: [["t_5", "read"]] },
  { account: "jang@corp.com", username: "장원석", invitationStatus: "invite_redeemed", sessionStatus: "online", teams: [["t_2", "read"]] },
  { account: "lim@corp.com", username: "임재현", invitationStatus: "invite_redeemed", sessionStatus: "online", teams: [["t_3", "write"]] },
  { account: "han@corp.com", username: "한지민", invitationStatus: "invite_redeemed", sessionStatus: "offline", teams: [["t_1", "read"]] },
  { account: "oh@corp.com", username: "오세영", invitationStatus: "invite_pending", sessionStatus: "offline", teams: [["t_4", "write"]] },
  // invite_redeemed + offline: code used (token released) but the agent has
  // never authenticated — the "초대 코드 사용됨 · 연결 대기 중" state.
  { account: "seo@corp.com", username: "서준호", invitationStatus: "invite_redeemed", sessionStatus: "offline", teams: [["t_5", "read"]] },
  { account: "shin@corp.com", username: "신동혁", invitationStatus: "invite_redeemed", sessionStatus: "online", teams: [] },
];

const seedUsersAndMemberships = (): {
  users: User[];
  memberships: Membership[];
} => {
  const users: User[] = [];
  const memberships: Membership[] = [];
  USER_SEED.forEach((s, i) => {
    const id = `u_${i + 1}`;
    users.push({
      id,
      account: s.account,
      username: s.username,
      invitationStatus: s.invitationStatus,
      sessionStatus: s.sessionStatus,
      lastAccessAt: s.sessionStatus === "online" ? daysAgo(i % 5) : null,
      // Any offline member that once redeemed but is not currently online may
      // carry an invited stamp; pending/expired always do.
      lastInvitedAt:
        s.invitationStatus === "invite_pending" ||
        s.invitationStatus === "invite_expired" ||
        s.invitationStatus === "invite_redeemed"
          ? daysAgo((i % 4) + 1)
          : null,
      // A redeemed member that is now offline models a destroyed session.
      sessionExpiredAt:
        s.invitationStatus === "invite_redeemed" && s.sessionStatus === "offline"
          ? daysAgo(i % 3)
          : null,
    });
    for (const [teamId, role] of s.teams)
      memberships.push({
        userId: id,
        teamId,
        role,
        joinedAt: daysAgo((i % 6) + 1),
      });
  });
  return { users, memberships };
};

const seedInvitations = (): InvitationRow[] => [
  {
    account: "park@corp.com",
    username: "박민준",
    issuedAt: daysAgo(3),
    lastAccessAt: daysAgo(2),
  },
  {
    account: "yoon@corp.com",
    username: "윤아름",
    issuedAt: daysAgo(2),
    lastAccessAt: null,
  },
  {
    account: "oh@corp.com",
    username: "오세영",
    issuedAt: daysAgo(1),
    lastAccessAt: null,
  },
  {
    account: "kang@corp.com",
    username: "강호진",
    issuedAt: daysAgo(6),
    lastAccessAt: daysAgo(5),
  },
  {
    account: "park@corp.com",
    username: "박민준",
    issuedAt: daysAgo(1),
    lastAccessAt: null,
  }, // second issuance
];

const freshSession = (): Session =>
  config.startLoggedIn
    ? {
        loggedIn: true,
        expiresAt: isoIn(config.sessionTtlMs),
        plan: "free",
        me: ADMIN,
      }
    : { loggedIn: false, expiresAt: null, plan: "free", me: null };

const buildState = (): State => {
  const { users, memberships } = seedUsersAndMemberships();
  return {
    teams: seedTeams(),
    memberships,
    users,
    invitations: seedInvitations(),
    workspace: {
      exists: true,
      phase: "running",
      endpointUrl: "https://mock-abc123.workspace.runespace.cloud:443",
      rows: 840,
      createdAt: daysAgo(12),
      orphaned: false,
    },
    workspaceFail: new Map(),
    session: freshSession(),
    pendingAuthStates: new Set<string>(),
    seq: 100,
  };
};

export let state: State = buildState();

export const resetState = (): void => {
  clearScheduledTransitions();
  state = buildState();
};

export const adminIdentity = (): Session["me"] => ADMIN;

export const nextId = (prefix: string): string => {
  state.seq += 1;
  return `${prefix}_${state.seq}`;
};

// ---- session ---------------------------------------------------------------

/**
 * getSession returns the live session, applying lazy expiry: a session whose
 * expiresAt has passed is dropped on read (as documented for the console).
 */
export const getSession = (): Session => {
  const s = state.session;
  if (
    s.loggedIn &&
    s.expiresAt !== null &&
    Date.parse(s.expiresAt) <= Date.now()
  ) {
    state.session = { loggedIn: false, expiresAt: null, me: null };
  }
  return state.session;
};

export const login = (): Session => {
  state.session = {
    loggedIn: true,
    expiresAt: isoIn(config.sessionTtlMs),
    me: ADMIN,
  };
  return state.session;
};

export const logout = (): void => {
  state.session = { loggedIn: false, expiresAt: null, me: null };
};

export const expireSessionNow = (): void => {
  if (state.session.loggedIn) state.session.expiresAt = isoIn(-1000);
};

/** touchSession implements optional sliding expiry (extends on activity). */
export const touchSession = (): void => {
  if (config.sliding && state.session.loggedIn) {
    state.session.expiresAt = isoIn(config.sessionTtlMs);
  }
};

// ---- workspace failure injection (dev-only) --------------------------------

/** armWorkspaceFail queues a one-shot failure for the next `op` call. */
export const armWorkspaceFail = (
  op: string,
  status: number,
  code: string,
): void => {
  state.workspaceFail.set(op, { status, code });
};

/**
 * consumeWorkspaceFail returns and clears an armed failure for `op` (one-shot),
 * or null if none is armed.
 */
export const consumeWorkspaceFail = (
  op: string,
): { status: number; code: string } | null => {
  const armed = state.workspaceFail.get(op);
  if (!armed) return null;
  state.workspaceFail.delete(op);
  return armed;
};

/**
 * markWorkspaceOrphaned simulates a console reinstall: the workspace's stored
 * team_secret fingerprint no longer matches, so GET /workspace reports
 * orphaned=true until the delete-and-recreate flow runs.
 */
export const markWorkspaceOrphaned = (): void => {
  if (!state.workspace.exists) {
    throw new HttpError(404, "WORKSPACE_NOT_FOUND", "no workspace exists");
  }
  state.workspace.orphaned = true;
};

// ---- workspace phase transitions -------------------------------------------

let transitions: ReturnType<typeof setTimeout>[] = [];

const clearScheduledTransitions = (): void => {
  for (const t of transitions) clearTimeout(t);
  transitions = [];
};

/**
 * scheduleWorkspacePhase flips the workspace to `to` after the configured
 * delay, simulating an async cloud operation. `onDone` runs after the flip.
 */
export const scheduleWorkspacePhase = (
  to: WorkspacePhase,
  onDone?: () => void,
): void => {
  const handle = setTimeout(() => {
    if (!state.workspace.exists && to !== "deleting") return;
    state.workspace.phase = to;
    if (to === "running" && state.workspace.rows === null)
      state.workspace.rows = 0;
    onDone?.();
  }, config.phaseDelayMs);
  transitions.push(handle);
};

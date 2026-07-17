import type { TTeamNode, TTeamTree } from "@/types/teamTypes";

/**
 * Dummy fixtures shaped exactly like the console API wire format.
 * Placeholder data for TeamsPage until the BFF endpoints are live — do
 * not ship usages.
 *
 * Shapes covered:
 * - GET /teams/tree            (Teams — flat nodes, client builds the tree)
 * - GET /users                 (Users — cross-team global list, paginated)
 * - GET /teams/{teamId}/members (Team members — per-team member table, paginated)
 */

/** Member status enum on the wire (common contract). */
export type TApiMemberStatus =
  "online" | "invite_pending" | "invite_expired" | "session_expired";

/** Roles grantable to members (common contract — Admin is console-account only). */
export type TApiMemberRole = "edit" | "write" | "read";

/** One membership entry inside a GET /users item (Users). */
export type TApiUserMembership = {
  teamId: string;
  teamName: string;
  role: TApiMemberRole;
};

/** One item of GET /users (Users). */
export type TApiUser = {
  userId: string;
  account: string;
  status: TApiMemberStatus;
  memberships: TApiUserMembership[];
  lastAccessAt: string | null;
  lastInvitedAt: string | null;
  sessionExpiredAt: string | null;
};

/** One item of GET /teams/{teamId}/members (Team members). */
export type TApiTeamMember = {
  userId: string;
  account: string;
  role: TApiMemberRole;
  status: TApiMemberStatus;
  joinedAt: string | null;
};

/** Common paginated envelope (common contract — { total, page, size, items }). */
export type TApiPage<T> = {
  total: number;
  page: number;
  size: number;
  items: T[];
};

/** Build a flat tree node — childCount always mirrors childrenIds. */
const team = (
  id: string,
  name: string,
  parentId: string | null,
  childrenIds: string[],
  memberCount: number,
): TTeamNode => ({
  id,
  name,
  parentId,
  childrenIds,
  childCount: childrenIds.length,
  memberCount,
});

/**
 * GET /teams/tree — 45 teams, 8 roots, up to 11 levels deep (load-test
 * sized: wide enough to overflow the org chart horizontally).
 * The original six (t_a~t_f) keep memberCounts consistent with
 * DUMMY_USERS; the load-test teams below are not reflected there.
 */
export const DUMMY_TEAMS_TREE: TTeamTree = [
  // ── original fixture ──────────────────────────────────────────────
  team("t_a", "플랫폼", null, ["t_b", "t_c", "t_mob"], 2),
  team("t_b", "백엔드", "t_a", ["t_api", "t_wrk", "t_db"], 6),
  team("t_c", "프론트엔드", "t_a", ["t_con", "t_ds"], 2),
  team("t_d", "디자인", null, [], 2),
  team("t_e", "보안", null, ["t_f", "t_cmp", "t_iam"], 2),
  team("t_f", "볼트", "t_e", ["t_fhe", "t_key"], 1),
  // ── load-test teams ───────────────────────────────────────────────
  team("t_mob", "모바일", "t_a", ["t_ios", "t_and"], 4),
  // long name — exercises node truncation + title tooltip
  team("t_api", "API 게이트웨이 익스피리언스", "t_b", ["t_gw", "t_gql"], 5),
  team("t_wrk", "워커", "t_b", [], 3),
  team("t_db", "데이터베이스", "t_b", [], 2),
  team("t_gw", "게이트웨이", "t_api", ["t_edge"], 2),
  // ── deep chain under Gateway (vertical overflow test, depth 11) ────
  team("t_edge", "엣지", "t_gw", ["t_rte"], 2),
  team("t_rte", "라우팅", "t_edge", ["t_lb"], 1),
  team("t_lb", "로드밸런서", "t_rte", ["t_rl"], 2),
  team("t_rl", "속도 제한", "t_lb", ["t_cch"], 1),
  team("t_cch", "캐싱", "t_rl", ["t_cdn"], 2),
  team("t_cdn", "콘텐츠 전송", "t_cch", ["t_pop"], 3),
  team("t_pop", "서울 거점", "t_cdn", [], 2),
  team("t_gql", "쿼리 API", "t_api", [], 3),
  team("t_con", "콘솔", "t_c", [], 4),
  team("t_ds", "디자인 시스템", "t_c", [], 2),
  team("t_ios", "아이폰 앱", "t_mob", [], 2),
  team("t_and", "안드로이드 앱", "t_mob", [], 2),
  team("t_cmp", "컴플라이언스", "t_e", [], 3),
  team("t_iam", "접근 제어", "t_e", [], 2),
  team("t_fhe", "동형암호 코어", "t_f", [], 4),
  team("t_key", "키 관리", "t_f", [], 2),
  team("t_data", "데이터", null, ["t_ana", "t_ml"], 1),
  team("t_ana", "분석", "t_data", [], 3),
  team("t_ml", "머신러닝", "t_data", ["t_trn", "t_inf"], 2),
  team("t_trn", "학습", "t_ml", [], 3),
  team("t_inf", "추론", "t_ml", [], 2),
  team("t_infra", "인프라", null, ["t_sre", "t_net", "t_obs"], 2),
  team("t_sre", "사이트 신뢰성", "t_infra", [], 4),
  team("t_net", "네트워크", "t_infra", [], 2),
  team("t_obs", "관측성", "t_infra", [], 3),
  team("t_prod", "프로덕트", null, ["t_bil", "t_onb"], 5),
  team("t_bil", "결제", "t_prod", [], 2),
  team("t_onb", "온보딩", "t_prod", [], 1),
  team("t_gro", "그로스", null, ["t_mkt", "t_sal"], 1),
  team("t_mkt", "마케팅", "t_gro", [], 3),
  team("t_sal", "영업", "t_gro", [], 2),
  team("t_cs", "고객 성공", null, ["t_sup", "t_edu"], 2),
  team("t_sup", "지원", "t_cs", [], 3),
  team("t_edu", "교육", "t_cs", [], 2),
];

/**
 * GET /users?page=1&size=10 — 23 users over 3 pages, covering all four
 * member statuses. Per-status timestamp display (Users): online →
 * lastAccessAt / pending·expired → lastInvitedAt / session_expired →
 * sessionExpiredAt. u_9~u_11 are also team B members (mirrored in
 * DUMMY_TEAM_B_MEMBERS); u_16 exercises the "+n" membership overflow
 * chip and u_17 the long-account truncation.
 */
export const DUMMY_USERS: TApiPage<TApiUser> = {
  total: 23,
  page: 1,
  size: 10,
  items: [
    {
      userId: "u_1",
      account: "k@corp.com",
      status: "online",
      memberships: [
        { teamId: "t_a", teamName: "플랫폼", role: "edit" },
        { teamId: "t_b", teamName: "백엔드", role: "edit" },
        { teamId: "t_c", teamName: "프론트엔드", role: "edit" },
      ],
      lastAccessAt: "2026-07-07T08:12:00Z",
      lastInvitedAt: "2026-07-06T09:00:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_2",
      account: "m@corp.com",
      status: "invite_pending",
      memberships: [{ teamId: "t_b", teamName: "백엔드", role: "read" }],
      lastAccessAt: null,
      lastInvitedAt: "2026-07-05T18:20:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_3",
      account: "n@corp.com",
      status: "invite_expired",
      memberships: [{ teamId: "t_b", teamName: "백엔드", role: "write" }],
      lastAccessAt: null,
      lastInvitedAt: "2026-07-03T10:00:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_4",
      account: "p@corp.com",
      status: "invite_pending",
      memberships: [{ teamId: "t_c", teamName: "프론트엔드", role: "read" }],
      lastAccessAt: null,
      lastInvitedAt: "2026-07-06T15:40:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_5",
      account: "q@corp.com",
      status: "invite_expired",
      memberships: [
        { teamId: "t_a", teamName: "플랫폼", role: "read" },
        { teamId: "t_d", teamName: "디자인", role: "read" },
      ],
      lastAccessAt: null,
      lastInvitedAt: "2026-07-02T09:30:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_6",
      account: "r@corp.com",
      status: "session_expired",
      memberships: [{ teamId: "t_e", teamName: "보안", role: "write" }],
      lastAccessAt: "2026-07-04T11:30:00Z",
      lastInvitedAt: "2026-07-01T10:10:00Z",
      sessionExpiredAt: "2026-07-06T17:05:00Z",
    },
    {
      userId: "u_7",
      account: "s@corp.com",
      status: "online",
      memberships: [
        { teamId: "t_d", teamName: "디자인", role: "edit" },
        { teamId: "t_f", teamName: "볼트", role: "read" },
      ],
      lastAccessAt: "2026-07-06T18:40:00Z",
      lastInvitedAt: "2026-07-05T15:02:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_8",
      account: "t@corp.com",
      status: "online",
      memberships: [{ teamId: "t_e", teamName: "보안", role: "read" }],
      lastAccessAt: "2026-07-07T07:55:00Z",
      lastInvitedAt: "2026-07-04T11:30:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_9",
      account: "u@corp.com",
      status: "online",
      memberships: [{ teamId: "t_b", teamName: "백엔드", role: "write" }],
      lastAccessAt: "2026-07-06T21:10:00Z",
      lastInvitedAt: "2026-07-01T09:20:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_10",
      account: "v@corp.com",
      status: "invite_pending",
      memberships: [{ teamId: "t_b", teamName: "백엔드", role: "read" }],
      lastAccessAt: null,
      lastInvitedAt: "2026-07-06T10:20:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_11",
      account: "w@corp.com",
      status: "session_expired",
      memberships: [
        { teamId: "t_b", teamName: "백엔드", role: "edit" },
        { teamId: "t_gql", teamName: "쿼리 API", role: "read" },
      ],
      lastAccessAt: "2026-07-03T13:40:00Z",
      lastInvitedAt: "2026-06-28T09:00:00Z",
      sessionExpiredAt: "2026-07-05T09:00:00Z",
    },
    {
      userId: "u_12",
      account: "x@corp.com",
      status: "online",
      memberships: [{ teamId: "t_con", teamName: "콘솔", role: "edit" }],
      lastAccessAt: "2026-07-07T06:45:00Z",
      lastInvitedAt: "2026-07-02T14:00:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_13",
      account: "y@corp.com",
      status: "online",
      memberships: [
        { teamId: "t_sre", teamName: "사이트 신뢰성", role: "write" },
        { teamId: "t_obs", teamName: "관측성", role: "read" },
      ],
      lastAccessAt: "2026-07-06T23:50:00Z",
      lastInvitedAt: "2026-07-01T08:10:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_14",
      account: "z@corp.com",
      status: "invite_pending",
      memberships: [
        { teamId: "t_fhe", teamName: "동형암호 코어", role: "read" },
      ],
      lastAccessAt: null,
      lastInvitedAt: "2026-07-07T08:30:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_15",
      account: "aa@corp.com",
      status: "invite_expired",
      memberships: [{ teamId: "t_mkt", teamName: "마케팅", role: "read" }],
      lastAccessAt: null,
      lastInvitedAt: "2026-06-25T16:00:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_16",
      account: "ab@corp.com",
      status: "online",
      memberships: [
        { teamId: "t_mob", teamName: "모바일", role: "edit" },
        { teamId: "t_ios", teamName: "아이폰 앱", role: "write" },
        { teamId: "t_and", teamName: "안드로이드 앱", role: "write" },
        { teamId: "t_con", teamName: "콘솔", role: "read" },
        { teamId: "t_ds", teamName: "디자인 시스템", role: "read" },
      ],
      lastAccessAt: "2026-07-05T14:20:00Z",
      lastInvitedAt: "2026-06-30T11:00:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_17",
      account: "external.partner.jihoon.kim@verylong-partner-domain.co.kr",
      status: "online",
      memberships: [{ teamId: "t_ana", teamName: "분석", role: "read" }],
      lastAccessAt: "2026-07-04T09:12:00Z",
      lastInvitedAt: "2026-07-01T10:30:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_18",
      account: "ac@corp.com",
      status: "session_expired",
      memberships: [{ teamId: "t_net", teamName: "네트워크", role: "write" }],
      lastAccessAt: "2026-06-30T18:25:00Z",
      lastInvitedAt: "2026-06-27T09:40:00Z",
      sessionExpiredAt: "2026-07-02T09:40:00Z",
    },
    {
      userId: "u_19",
      account: "ad@corp.com",
      status: "invite_pending",
      memberships: [{ teamId: "t_prod", teamName: "프로덕트", role: "read" }],
      lastAccessAt: null,
      lastInvitedAt: "2026-07-05T11:00:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_20",
      account: "ae@corp.com",
      status: "online",
      memberships: [
        { teamId: "t_trn", teamName: "학습", role: "edit" },
        { teamId: "t_inf", teamName: "추론", role: "edit" },
      ],
      lastAccessAt: "2026-07-06T16:05:00Z",
      lastInvitedAt: "2026-07-03T13:15:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_21",
      account: "af@corp.com",
      status: "invite_expired",
      memberships: [{ teamId: "t_key", teamName: "키 관리", role: "read" }],
      lastAccessAt: null,
      lastInvitedAt: "2026-06-24T15:30:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_22",
      account: "ag@corp.com",
      status: "online",
      memberships: [{ teamId: "t_gw", teamName: "게이트웨이", role: "write" }],
      lastAccessAt: "2026-07-07T05:58:00Z",
      lastInvitedAt: "2026-07-04T10:05:00Z",
      sessionExpiredAt: null,
    },
    {
      userId: "u_23",
      account: "ah@corp.com",
      status: "invite_pending",
      memberships: [{ teamId: "t_edu", teamName: "교육", role: "read" }],
      lastAccessAt: null,
      lastInvitedAt: "2026-07-06T19:45:00Z",
      sessionExpiredAt: null,
    },
  ],
};

/**
 * GET /teams/t_b/members?page=1&size=10 — team Backend's member table
 * (extends the SC-06 wireframe fixture; roles/statuses mirror the t_b
 * memberships in DUMMY_USERS). joinedAt is null until the invite is
 * accepted (wireframe shows "—").
 */
export const DUMMY_TEAM_B_MEMBERS: TApiPage<TApiTeamMember> = {
  total: 6,
  page: 1,
  size: 10,
  items: [
    {
      userId: "u_1",
      account: "k@corp.com",
      role: "edit",
      status: "online",
      joinedAt: "2026-07-02T00:00:00Z",
    },
    {
      userId: "u_2",
      account: "m@corp.com",
      role: "read",
      status: "invite_pending",
      joinedAt: null,
    },
    {
      userId: "u_3",
      account: "n@corp.com",
      role: "write",
      status: "invite_expired",
      joinedAt: null,
    },
    {
      userId: "u_9",
      account: "u@corp.com",
      role: "write",
      status: "online",
      joinedAt: "2026-07-02T00:00:00Z",
    },
    {
      userId: "u_10",
      account: "v@corp.com",
      role: "read",
      status: "invite_pending",
      joinedAt: null,
    },
    {
      userId: "u_11",
      account: "w@corp.com",
      role: "edit",
      status: "session_expired",
      joinedAt: "2026-06-29T00:00:00Z",
    },
  ],
};

// Team endpoints (SC-05~09): tree, detail, create, rename, delete.
import { HttpError, sendJson, sendNoContent } from "../http.ts";
import type { Ctx } from "../router.ts";
import { nextId, state } from "../store.ts";
import type { Team } from "../types.ts";

const childrenOf = (teamId: string): Team[] =>
  state.teams.filter((t) => t.parentId === teamId);

const memberCountOf = (teamId: string): number =>
  state.memberships.filter((m) => m.teamId === teamId).length;

const findTeam = (id: string): Team => {
  const team = state.teams.find((t) => t.id === id);
  if (!team) throw new HttpError(404, "TEAM_NOT_FOUND", `team ${id} not found`);
  return team;
};

const detail = (t: Team) => ({
  id: t.id,
  name: t.name,
  parentId: t.parentId,
  children: childrenOf(t.id).map((c) => c.id),
  memberCount: memberCountOf(t.id),
  createdAt: t.createdAt,
});

export const getTeamsTree = (ctx: Ctx): void => {
  const nodes = state.teams.map((t) => {
    const children = childrenOf(t.id);
    return {
      id: t.id,
      name: t.name,
      parentId: t.parentId,
      childrenIds: children.map((c) => c.id),
      childCount: children.length,
      memberCount: memberCountOf(t.id),
    };
  });
  sendJson(ctx.res, 200, nodes);
};

export const getTeam = (ctx: Ctx): void => {
  sendJson(ctx.res, 200, detail(findTeam(ctx.params.teamId)));
};

// Mirrors the console client's TEAM_NAME_PATTERN and the backend
// validateGroupName rule: digits, Latin letters, Hangul, '-' '_' only, max 50.
const NAME_RE = /^[0-9A-Za-z가-힣_-]{1,50}$/;

const validateName = (name: unknown): string => {
  if (
    typeof name !== "string" ||
    name.trim() === "" ||
    !NAME_RE.test(name.trim())
  ) {
    throw new HttpError(
      400,
      "TEAM_NAME_INVALID",
      "team name violates naming rules",
    );
  }
  return name.trim();
};

const assertUniqueSibling = (
  name: string,
  parentId: string | null,
  excludeId?: string,
): void => {
  const clash = state.teams.some(
    (t) =>
      t.parentId === parentId &&
      t.id !== excludeId &&
      t.name.toLowerCase() === name.toLowerCase(),
  );
  if (clash) {
    throw new HttpError(
      409,
      "TEAM_NAME_DUPLICATE",
      "a sibling team already has this name",
    );
  }
};

export const createTeam = (ctx: Ctx): void => {
  const body = (ctx.body ?? {}) as { name?: unknown; parentId?: unknown };
  const name = validateName(body.name);
  const parentId =
    body.parentId === undefined || body.parentId === null
      ? null
      : String(body.parentId);
  if (parentId !== null) findTeam(parentId); // 404 if parent missing
  assertUniqueSibling(name, parentId);
  const team: Team = {
    id: nextId("t"),
    name,
    parentId,
    createdAt: new Date().toISOString(),
  };
  state.teams.push(team);
  sendJson(ctx.res, 201, detail(team));
};

export const renameTeam = (ctx: Ctx): void => {
  const team = findTeam(ctx.params.teamId);
  const body = (ctx.body ?? {}) as { name?: unknown };
  const name = validateName(body.name);
  assertUniqueSibling(name, team.parentId, team.id);
  team.name = name;
  sendJson(ctx.res, 200, detail(team));
};

export const deleteTeam = (ctx: Ctx): void => {
  const team = findTeam(ctx.params.teamId);
  const memoryAction = ctx.query.get("memoryAction");
  const targetTeamId = ctx.query.get("targetTeamId");

  if (childrenOf(team.id).length > 0) {
    throw new HttpError(409, "TEAM_HAS_CHILDREN", "team has child teams");
  }
  if (memoryAction !== "transfer" && memoryAction !== "purge") {
    throw new HttpError(
      400,
      "VALIDATION_ERROR",
      "memoryAction must be transfer|purge",
    );
  }
  if (memoryAction === "transfer") {
    if (!targetTeamId || targetTeamId === team.id) {
      throw new HttpError(
        400,
        "VALIDATION_ERROR",
        "transfer requires a distinct targetTeamId",
      );
    }
    findTeam(targetTeamId); // 404 if target missing
    // Memory transfer is internal server work; nothing observable in the mock.
  }
  // Remove the team and its memberships (the team's own memberships only).
  state.teams = state.teams.filter((t) => t.id !== team.id);
  state.memberships = state.memberships.filter((m) => m.teamId !== team.id);
  sendNoContent(ctx.res);
};

import { useState } from "react";
import { useSearchParams } from "react-router";

import Button from "@/components/elements/Button";
import Feedback from "@/components/elements/Feedback";
import SearchInput from "@/components/elements/SearchInput";
import CreateTeamModal from "@/components/teams/CreateTeamModal";
import OrgChart from "@/components/teams/OrgChart";
import TreeDetailView from "@/components/teams/TreeDetailView";
import { useCreateTeamMutation } from "@/hooks/mutations/useTeamMutations";
import { useTeamsTreeQuery } from "@/hooks/queries/useTeamsTreeQuery";
import { parseErrorCode } from "@/api/parseError";
import { cn } from "@/utils/cn";
import { BTN_TEXT } from "@/constants/commonConstants";
import { L } from "@/locales";
import { useNoticeStore } from "@/stores/noticeStore";

/** Create-team error codes → SC-07 copy (shared with TreeDetailView). */
const CREATE_TEAM_REASON: Record<string, string> = {
  TEAM_NAME_DUPLICATE: L.teams.dupName,
  TEAM_NAME_INVALID: L.teams.invalidTeamName,
};

const feedbackPanel =
  "m-6 flex min-h-[340px] flex-col items-center justify-center gap-3 text-center";

const styles = {
  panel: "flex flex-col",
  /* SC-06 header strip: view toggle (트리·상세 | 조직도) + team search.
     sticky: stays pinned to the viewport top if the page ever scrolls
     (bg so content doesn't bleed through underneath). */
  header:
    "border-border bg-background sticky top-0 z-10 flex items-center gap-2 border-b px-4 py-2.5",
  segment:
    "border-border-strong inline-flex overflow-hidden rounded-[8px] border text-sm",
  segmentOn: "bg-foreground text-background px-3 py-1 font-semibold",
  segmentOff:
    "text-muted-foreground cursor-pointer px-3 py-1 hover:text-foreground",
};

type TTeamsView = "tree" | "org";

/**
 * TeamsPage is the team management screen (SC-06): the view toggle
 * switches between 트리·상세 (TreeDetailView) and 조직도 (OrgChart),
 * with the team search shared by both views.
 *
 * View and selection live in the URL (?view=tree&team=t_1) so refresh,
 * back/forward, and deep links preserve them; defaults (조직도, first
 * root team) are omitted to keep /teams clean. The search text stays
 * local — as-you-type state would churn history and isn't worth
 * deep-linking.
 */
const TeamsPage = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const [teamSearch, setTeamSearch] = useState("");
  const { data: teams, isPending, isError } = useTeamsTreeQuery();

  /* SC-06 state B (팀 0개) create action — the tree panel's [새 팀 만들기]
     is gone when there are no teams, so the empty panel owns the create
     flow (same mutation/error mapping as TreeDetailView's SC-07). */
  const [createOpen, setCreateOpen] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const createTeam = useCreateTeamMutation();
  const showNotice = useNoticeStore((s) => s.showNotice);

  const handleCreate = (name: string, parentId: string | null) => {
    setCreateError(null);
    createTeam.mutate(
      { name, parentId },
      {
        onSuccess: () => {
          setCreateOpen(false);
          showNotice(L.teams.createTeamTitle, L.teams.teamCreated, "success");
        },
        onError: async (res) => {
          const code = await parseErrorCode(res);
          setCreateError(CREATE_TEAM_REASON[code] ?? L.teams.createTeamFailed);
        },
      },
    );
  };

  /* 트리·상세 is the entry view (its first top-level team auto-selected);
     조직도 is reached by the view toggle. */
  const view: TTeamsView = searchParams.get("view") === "org" ? "org" : "tree";

  const teamIds = new Set((teams ?? []).map((t) => t.id));
  /* SC-06 entry rule: the first top-level team is auto-selected. Also the
     fallback for a stale/invalid ?team= (e.g. the team was deleted). */
  const firstRootId = (teams ?? []).find((t) => t.parentId === null)?.id ?? "";
  const teamParam = searchParams.get("team");
  const selectedTeamId =
    teamParam && teamIds.has(teamParam) ? teamParam : firstRootId;

  /* null deletes a key; other params (e.g. the ?teams= state previews)
     pass through untouched. One call = one history entry. */
  const updateParams = (updates: Record<string, string | null>) =>
    setSearchParams((prev) => {
      const next = new URLSearchParams(prev);
      for (const [key, value] of Object.entries(updates)) {
        if (value === null) next.delete(key);
        else next.set(key, value);
      }
      return next;
    });

  const setView = (next: TTeamsView) =>
    updateParams({ view: next === "tree" ? null : next });

  const selectTeam = (teamId: string) =>
    updateParams({ team: teamId === firstRootId ? null : teamId });

  /* Shared selection — the org chart selects and hands off to 트리·상세
     (SC-05 node click → SC-06 entry) in a single history entry. */
  const handleOrgSelect = (teamId: string) =>
    updateParams({
      team: teamId === firstRootId ? null : teamId,
      view: null,
    });

  if (isPending) {
    return (
      <section className={styles.panel} aria-label={L.nav.teams}>
        <div className={styles.header} />
      </section>
    );
  }
  if (isError) {
    return (
      <section className={styles.panel} aria-label={L.nav.teams}>
        <Feedback
          state="error"
          title={L.teams.teamsLoadError}
          description={L.common.refreshRetry}
          className={feedbackPanel}
          action={
            <Button
              btnText={BTN_TEXT.refresh}
              btnSize="sm"
              btnColor="grayOutline"
              className="w-fit"
              handleClick={() => window.location.reload()}
            />
          }
        />
      </section>
    );
  }

  return (
    <section className={styles.panel} aria-label={L.nav.teams}>
      <div className={styles.header}>
        <div
          className={styles.segment}
          role="group"
          aria-label={L.teams.switchView}
        >
          <button
            type="button"
            className={cn(
              view === "tree" ? styles.segmentOn : styles.segmentOff,
            )}
            aria-pressed={view === "tree"}
            onClick={() => setView("tree")}
          >
            {L.teams.treeDetail}
          </button>
          <button
            type="button"
            className={cn(
              view === "org" ? styles.segmentOn : styles.segmentOff,
            )}
            aria-pressed={view === "org"}
            onClick={() => setView("org")}
          >
            {L.teams.orgChartView}
          </button>
        </div>
        {/* Nothing to search when there are no teams (SC-06 state B). */}
        {teams.length > 0 && (
          <SearchInput
            value={teamSearch}
            onChange={setTeamSearch}
            placeholder={L.teams.searchTeams}
            maxLength={50}
            className="ml-auto w-55"
          />
        )}
      </div>

      {teams.length === 0 ? (
        <Feedback
          state="empty"
          title={L.teams.emptyTitle}
          description={L.teams.emptyDesc}
          className={feedbackPanel}
          action={
            <Button
              btnText={BTN_TEXT.createTeam}
              btnSize="md"
              btnColor="mintFilled"
              className="w-fit"
              handleClick={() => {
                setCreateError(null);
                setCreateOpen(true);
              }}
            />
          }
        />
      ) : view === "tree" ? (
        <TreeDetailView
          teams={teams}
          teamSearch={teamSearch}
          selectedTeamId={selectedTeamId}
          onSelectTeam={selectTeam}
        />
      ) : (
        <OrgChart
          teams={teams}
          query={teamSearch}
          onSelectTeam={handleOrgSelect}
        />
      )}

      {createOpen && (
        <CreateTeamModal
          teams={teams}
          error={createError}
          onClose={() => setCreateOpen(false)}
          onCreate={handleCreate}
        />
      )}
    </section>
  );
};

export default TeamsPage;

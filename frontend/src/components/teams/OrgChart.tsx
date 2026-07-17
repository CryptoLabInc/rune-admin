import { useMemo, useState } from "react";

import { cn } from "@/utils/cn";
import type { TTeamNode, TTeamTree } from "@/types/teamTypes";

const styles = {
  /* Viewport-bound pannable window: height caps at the screen minus the
     app chrome (navbar 57 + layout py 48 + view header 53 + footer 53
     ≈ 212px), so the chart scrolls itself on BOTH axes and the page
     never scrolls — header and footer stay in view. Themed scrollbar
     via ::-webkit-scrollbar (rounded ends need the pseudo-elements;
     Chromium ignores them if scrollbar-color is set). */
  orgScroll: cn(
    "h-[calc(100vh-212px)] overflow-auto",
    "[&::-webkit-scrollbar]:h-2 [&::-webkit-scrollbar]:w-2",
    "[&::-webkit-scrollbar-track]:rounded-full [&::-webkit-scrollbar-track]:bg-[color-mix(in_srgb,var(--color-muted-foreground)_8%,transparent)]",
    "[&::-webkit-scrollbar-thumb]:rounded-full [&::-webkit-scrollbar-thumb]:bg-[color-mix(in_srgb,var(--color-mint)_32%,transparent)]",
    "[&::-webkit-scrollbar-thumb]:hover:bg-[color-mix(in_srgb,var(--color-mint)_45%,transparent)]",
    /* Where the two bars meet — defaults to a white box otherwise. */
    "[&::-webkit-scrollbar-corner]:bg-transparent",
  ),
  /* w-max/mx-auto inner list: centers while narrow, scrolls fully when
     wide (justify-center inside an overflow container would clip the
     left edge unreachably). */
  orgCanvas: "mx-auto flex w-max list-none items-start gap-10 p-6",
  /* Zoom controls overlay — anchored to the viewport frame (not the
     scrolling content), so they stay put while the chart pans. The
     offsets clear the 8px scrollbars (right-5/bottom-5 = bar + 12px)
     so the controls never sit against them. */
  orgViewport: "relative",
  zoomControls:
    "border-border bg-panel-solid/90 absolute right-5 bottom-5 z-10 flex items-center gap-1 rounded-[8px] border p-1",
  zoomButton:
    "border-border-strong bg-panel-solid hover:enabled:border-mint/60 hover:enabled:text-mint grid size-7 cursor-pointer place-items-center rounded-[6px] border font-mono text-sm transition-colors disabled:cursor-not-allowed disabled:opacity-30",
  zoomLabel: "text-faint w-11 text-center font-mono text-xs",
  orgItem: "flex flex-col items-center px-3",
  /* Bracket connectors (wireframe .org technique): each child draws the
     half-bars above itself with pseudo-element borders — before: = left
     half, after: = right half + the drop stem. First/last children trim
     their outer half so the bar only spans between siblings; an only
     child is first AND last, leaving just the stem. Siblings must NOT
     use flex gap (bars would break) — spacing comes from item px. */
  orgConnectors: cn(
    "relative pt-6",
    "before:border-border-strong before:absolute before:top-0 before:right-1/2 before:h-6 before:w-1/2 before:border-t before:content-['']",
    "after:border-border-strong after:absolute after:top-0 after:left-1/2 after:h-6 after:w-1/2 after:border-t after:border-l after:content-['']",
    "first:before:border-t-0 first:after:rounded-tl-md",
    "last:before:border-r last:before:rounded-tr-md last:after:border-t-0 last:after:border-l-0",
  ),
  /* An only child gets a plain straight stem — the first/last bar trims
     above would both apply to it and bend the line. */
  orgSoloConnector: cn(
    "relative pt-6",
    "before:border-border-strong before:absolute before:top-0 before:left-1/2 before:h-6 before:border-l before:content-['']",
  ),
  /* Uniform node size — long names truncate (full name via title).
     Clickable: selects the team and hands off to 트리·상세 (SC-05→06). */
  orgBox:
    "border-border-strong bg-panel-solid hover:bg-surface flex h-10 w-[150px] cursor-pointer items-center rounded-md border px-3 text-sm transition-colors",
  orgBoxName: "min-w-0 flex-1 truncate text-center",
  orgBoxMatch: "border-mint/70",
  orgStem: "bg-border-strong h-6 w-px",
  orgChildrenRow: "m-0 flex list-none items-start p-0",
};

/** Org chart zoom steps: 100% → 2x out (50%) → 3x out (33%). */
const ZOOM_SCALES = [1, 0.5, 1 / 3] as const;
const ZOOM_LABELS = ["100%", "50%", "33%"] as const;

/** Root-node accent palette (design-system accent tokens, 2차-A §1.3) —
    cycled by root index so each top-level team gets a distinct border.
    Deliberately excludes the mint/teal family: mint is the search-match
    highlight, so a teal root would read as a false match. */
const ROOT_BORDER_PALETTE = [
  "border-accent-blue/70",
  "border-cobalt/70",
  "border-warning/70",
  "border-negative/70",
  "border-subtle/70",
  "border-foreground/50",
] as const;

type TOrgConnector = "none" | "bars" | "stem";

const CONNECTOR_STYLE: Record<TOrgConnector, string | undefined> = {
  none: undefined,
  bars: styles.orgConnectors,
  stem: styles.orgSoloConnector,
};

/** Matching team ids for the query — the tree is never pruned; matches
    are only highlighted in place. null = no active query. */
const buildMatchSet = (teams: TTeamTree, query: string): Set<string> | null => {
  if (!query) return null;
  return new Set(
    teams
      .filter((team) => team.name.toLocaleLowerCase().includes(query))
      .map((team) => team.id),
  );
};

/**
 * OrgNode renders one 조직도 subtree (SC-05): the team box on top with
 * its children laid out in a row beneath, recursively — always fully
 * expanded.
 */
const OrgNode = ({
  node,
  connector = "bars",
  matched = null,
  teamsById,
  rootBorderById,
  onSelect,
}: {
  node: TTeamNode;
  /** none = root · bars = one of several siblings · stem = only child */
  connector?: TOrgConnector;
  matched?: Set<string> | null;
  /** Flat GET /teams/tree nodes by id — childrenIds hold ids, not nodes. */
  teamsById: Map<string, TTeamNode>;
  /** Root id → assigned accent border class. */
  rootBorderById: Map<string, string>;
  onSelect: (teamId: string) => void;
}) => {
  const children = node.childrenIds
    .map((id) => teamsById.get(id))
    .filter((child): child is TTeamNode => child !== undefined);
  const hasChildren = children.length > 0;
  const isMatch = matched?.has(node.id) ?? false;

  return (
    <li className={cn(styles.orgItem, CONNECTOR_STYLE[connector])}>
      <button
        type="button"
        className={cn(
          styles.orgBox,
          /* Roots get their assigned accent border; the search-match
             highlight (mint) still wins while a query is active. */
          connector === "none" && rootBorderById.get(node.id),
          isMatch && styles.orgBoxMatch,
        )}
        aria-label={`${node.name} 상세 보기`}
        onClick={() => onSelect(node.id)}
      >
        <b className={styles.orgBoxName} title={node.name}>
          {node.name}
        </b>
      </button>

      {hasChildren && (
        <>
          <div className={styles.orgStem} aria-hidden="true" />
          <ul className={styles.orgChildrenRow}>
            {children.map((child) => (
              <OrgNode
                key={child.id}
                node={child}
                connector={children.length === 1 ? "stem" : "bars"}
                matched={matched}
                teamsById={teamsById}
                rootBorderById={rootBorderById}
                onSelect={onSelect}
              />
            ))}
          </ul>
        </>
      )}
    </li>
  );
};

interface OrgChartProps {
  /** Flat GET /teams/tree nodes — owned by TeamsPage. */
  teams: TTeamTree;
  /** Team search text — owned by TeamsPage (header search input). */
  query: string;
  /** Node click — selects the team and switches to 트리·상세 (SC-06). */
  onSelectTeam: (teamId: string) => void;
}

/**
 * OrgChart is the SC-05 조직도 view: the team hierarchy as a bracket of
 * uniform boxes in a viewport-bound, 2D-scrollable canvas with zoom
 * controls. Search never prunes — the full tree stays rendered and
 * matching nodes are highlighted in place.
 */
const OrgChart = ({ teams, query, onSelectTeam }: OrgChartProps) => {
  const [zoomStep, setZoomStep] = useState(0);

  const teamsById = useMemo(
    () => new Map(teams.map((t) => [t.id, t])),
    [teams],
  );
  const rootTeams = useMemo(
    () => teams.filter((t) => t.parentId === null),
    [teams],
  );
  const rootBorderById = useMemo(
    () =>
      new Map(
        rootTeams.map((team, index) => [
          team.id,
          ROOT_BORDER_PALETTE[index % ROOT_BORDER_PALETTE.length],
        ]),
      ),
    [rootTeams],
  );

  const matched = useMemo(
    () => buildMatchSet(teams, query.trim().toLocaleLowerCase()),
    [teams, query],
  );

  return (
    <div className={styles.orgViewport}>
      <div className={styles.orgScroll}>
        <ul
          className={styles.orgCanvas}
          style={{ zoom: ZOOM_SCALES[zoomStep] }}
        >
          {rootTeams.map((team) => (
            <OrgNode
              key={team.id}
              node={team}
              connector="none"
              matched={matched}
              teamsById={teamsById}
              rootBorderById={rootBorderById}
              onSelect={onSelectTeam}
            />
          ))}
        </ul>
      </div>
      <div className={styles.zoomControls}>
        <button
          type="button"
          className={styles.zoomButton}
          aria-label="축소"
          disabled={zoomStep >= ZOOM_SCALES.length - 1}
          onClick={() => setZoomStep((step) => step + 1)}
        >
          −
        </button>
        <span className={styles.zoomLabel}>{ZOOM_LABELS[zoomStep]}</span>
        <button
          type="button"
          className={styles.zoomButton}
          aria-label="확대"
          disabled={zoomStep === 0}
          onClick={() => setZoomStep((step) => step - 1)}
        >
          +
        </button>
      </div>
    </div>
  );
};

export default OrgChart;

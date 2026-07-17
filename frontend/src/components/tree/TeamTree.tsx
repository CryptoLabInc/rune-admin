import { useMemo, useState } from "react";

import Feedback from "@/components/elements/Feedback";
import TreeNode from "@/components/tree/TreeNode";
import { cn } from "@/utils/cn";
import type { TTeamNode } from "@/types/commonTypes";

interface TeamTreeProps {
  teams: TTeamNode[];
  selectedId?: string;
  onSelect: (node: TTeamNode) => void;
  /** Case-insensitive filter — a node matches if it or a descendant matches. */
  query?: string;
  defaultExpandedIds?: string[];
  emptyTitle?: string;
  emptyDescription?: string;
  className?: string;
}

interface TFilterResult {
  nodes: TTeamNode[];
  /** Nodes kept only because a descendant matched — auto-expanded so the
      match is actually visible (2차-B spec: ancestors stay open). */
  autoExpandIds: string[];
}

/**
 * Prunes the tree to matching branches: a self-matching node keeps its
 * whole subtree (context); otherwise only children leading to a match
 * survive. `query` must already be trimmed + lowercased.
 */
const filterTree = (teams: TTeamNode[], query: string): TFilterResult => {
  const autoExpandIds: string[] = [];
  const prune = (node: TTeamNode): TTeamNode | null => {
    if (node.name.toLocaleLowerCase().includes(query)) return node;
    const children = (node.children ?? [])
      .map(prune)
      .filter((child): child is TTeamNode => child !== null);
    if (children.length === 0) return null;
    autoExpandIds.push(node.id);
    return { ...node, children };
  };
  return {
    nodes: teams.map(prune).filter((node): node is TTeamNode => node !== null),
    autoExpandIds,
  };
};

/**
 * TeamTree is the SC-06 team hierarchy list (ported from UIKIT
 * AdminTeamTree): recursive nodes with expand/collapse state and a
 * search filter. The panel frame, header, and search input around it
 * are assembled per screen. While a query is active, non-matching
 * branches are pruned, ancestors of matches auto-expand (the user's
 * manual expansion state is restored when the query clears), and the
 * matched name fragment is emphasized. An empty filter result shows
 * Feedback.
 */
const TeamTree = ({
  teams,
  selectedId,
  onSelect,
  query = "",
  defaultExpandedIds = [],
  emptyTitle = "검색 결과가 없습니다.",
  emptyDescription = "팀 이름을 다시 확인해 주세요.",
  className,
}: TeamTreeProps) => {
  const [expanded, setExpanded] = useState(() => new Set(defaultExpandedIds));
  const normalizedQuery = query.trim().toLocaleLowerCase();

  const { nodes: filtered, autoExpandIds } = useMemo(
    () =>
      normalizedQuery
        ? filterTree(teams, normalizedQuery)
        : { nodes: teams, autoExpandIds: [] },
    [teams, normalizedQuery],
  );

  /* Merged for rendering only — manual state survives the search. */
  const visibleExpanded = useMemo(
    () =>
      autoExpandIds.length > 0
        ? new Set([...expanded, ...autoExpandIds])
        : expanded,
    [expanded, autoExpandIds],
  );

  const toggle = (id: string) =>
    setExpanded((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });

  if (filtered.length === 0) {
    return (
      <Feedback
        state="empty"
        title={emptyTitle}
        description={emptyDescription}
      />
    );
  }
  return (
    <ul
      role="tree"
      className={cn(
        "m-0 min-h-[286px] list-none px-2 pt-[2px] pb-3",
        className,
      )}
    >
      {filtered.map((node) => (
        <TreeNode
          key={node.id}
          node={node}
          depth={0}
          selectedId={selectedId}
          expanded={visibleExpanded}
          onSelect={onSelect}
          onToggle={toggle}
          highlight={normalizedQuery}
        />
      ))}
    </ul>
  );
};

export default TeamTree;

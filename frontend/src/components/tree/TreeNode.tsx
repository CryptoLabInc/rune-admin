import type { CSSProperties } from "react";

import IconMinus from "@/components/icons/IconMinus";
import IconPlus from "@/components/icons/IconPlus";
import { cn } from "@/utils/cn";
import { L } from "@/locales";
import type { TTeamNode } from "@/types/commonTypes";

const styles = {
  row: "grid grid-cols-[24px_1fr] items-center rounded-sm pl-[calc(var(--tree-depth)*18px)] transition-[background-color] duration-[160ms]",
  rowHover: "hover:bg-muted-foreground/[4%]",
  rowSelected: "bg-mint/[7%] text-foreground shadow-[inset_2px_0] shadow-mint",
  toggle: "text-faint grid h-[30px] w-[22px] cursor-pointer place-items-center",
  leaf: "text-border-strong grid place-items-center",
  name: "text-muted-foreground flex min-w-0 cursor-pointer items-center justify-between gap-3 py-2.5 pr-2.5 pl-1 text-left text-base",
  nameSelected: "text-foreground",
};

interface TreeNodeProps {
  node: TTeamNode;
  depth: number;
  selectedId?: string;
  expanded: Set<string>;
  onSelect: (node: TTeamNode) => void;
  onToggle: (id: string) => void;
  /** Active search text (trimmed + lowercased) — emphasizes the match. */
  highlight?: string;
  className?: string;
}

/** Name with the matched fragment emphasized (weight, per typo rules). */
const HighlightedName = ({
  name,
  highlight,
}: {
  name: string;
  highlight?: string;
}) => {
  if (!highlight) return name;
  const start = name.toLocaleLowerCase().indexOf(highlight);
  if (start === -1) return name;
  const end = start + highlight.length;
  return (
    <>
      {name.slice(0, start)}
      <b className="text-foreground">{name.slice(start, end)}</b>
      {name.slice(end)}
    </>
  );
};

/**
 * TreeNode is one team-tree row (ported from UIKIT AdminTreeNode):
 * toggle (−/+) or leaf dot, name, and member count. Depth indents by
 * 18px per level via the --tree-depth CSS variable; children render
 * recursively while expanded.
 */
const TreeNode = ({
  node,
  depth,
  selectedId,
  expanded,
  onSelect,
  onToggle,
  highlight,
  className,
}: TreeNodeProps) => {
  const hasChildren = !!node.children?.length;
  const isOpen = expanded.has(node.id);
  const isActive = selectedId === node.id;
  return (
    <li>
      <div
        className={cn(
          styles.row,
          isActive ? styles.rowSelected : styles.rowHover,
          className,
        )}
        style={{ "--tree-depth": depth } as CSSProperties}
      >
        {hasChildren ? (
          <button
            type="button"
            className={styles.toggle}
            aria-label={
              isOpen
                ? L.teams.collapseName(node.name)
                : L.teams.expandName(node.name)
            }
            aria-expanded={isOpen}
            onClick={() => onToggle(node.id)}
          >
            {isOpen ? (
              <IconMinus className="size-2.5" />
            ) : (
              <IconPlus className="size-2.5" />
            )}
          </button>
        ) : (
          <span aria-hidden="true" className={styles.leaf}>
            ·
          </span>
        )}
        <button
          type="button"
          className={cn(styles.name, isActive && styles.nameSelected)}
          onClick={() => onSelect(node)}
        >
          <span className="truncate">
            <HighlightedName name={node.name} highlight={highlight} />
          </span>
        </button>
      </div>
      {hasChildren && isOpen && (
        <ul className="m-0 list-none p-0">
          {node.children?.map((child) => (
            <TreeNode
              key={child.id}
              node={child}
              depth={depth + 1}
              selectedId={selectedId}
              expanded={expanded}
              onSelect={onSelect}
              onToggle={onToggle}
              highlight={highlight}
            />
          ))}
        </ul>
      )}
    </li>
  );
};

export default TreeNode;

import { cn } from "@/utils/cn";
import type { TTeamNode } from "@/types/commonTypes";

const styles = {
  wrap: "bg-mint/[2%] grid grid-cols-[auto_1fr_auto] items-center gap-2.5 border-t px-4 py-3",
  label: "text-tag text-mint font-mono tracking-[0.09em]",
  name: "truncate text-sm",
  members: "text-tag text-faint font-mono",
};

interface TeamTreeFooterProps {
  node: TTeamNode;
  className?: string;
}

/**
 * TeamTreeFooter summarizes the current selection under the tree panel
 * (ported from UIKIT TeamTreeSelectedFooter): SELECTED label, team
 * name, member count.
 */
const TeamTreeFooter = ({ node, className }: TeamTreeFooterProps) => {
  return (
    <div className={cn(styles.wrap, className)}>
      <span className={styles.label}>SELECTED</span>
      <b className={styles.name}>{node.name}</b>
      <code className={styles.members}>{node.members} members</code>
    </div>
  );
};

export default TeamTreeFooter;

import Checkbox from "@/components/elements/Checkbox";
import Dropdown from "@/components/elements/Dropdown";
import TableCell from "@/components/table/TableCell";
import TableRow from "@/components/table/TableRow";
import { cn } from "@/utils/cn";
import { L } from "@/locales";
import type { TDropdownOption } from "@/types/commonTypes";

const styles = {
  changedBadge:
    "text-tag text-mint ml-2 align-middle font-mono font-semibold tracking-[0.1em] not-italic",
};

interface MembershipRowProps {
  name: string;
  role: string;
  roleOptions: TDropdownOption[];
  checked: boolean;
  /** Unsaved role change — pending accent + CHANGED badge (SC-13). */
  changed?: boolean;
  onCheck?: (checked: boolean) => void;
  onRoleChange?: (role: string) => void;
  className?: string;
}

/**
 * MembershipRow is one team membership row inside the member-detail
 * drawer's table (SC-13): checkbox, team name, and a compact role
 * dropdown — a TableRow so the drawer list shares the app's table
 * grammar (selected wash, changed accent). Clicking the team name
 * toggles the checkbox too (the name is the larger target); the role
 * dropdown stays independent.
 */
const MembershipRow = ({
  name,
  role,
  roleOptions,
  checked,
  changed = false,
  onCheck = () => {},
  onRoleChange = () => {},
  className,
}: MembershipRowProps) => {
  return (
    <TableRow selected={checked} changed={changed} className={className}>
      <TableCell className="w-8 pr-1">
        <Checkbox
          checked={checked}
          onChange={onCheck}
          ariaLabel={L.common.selectName(name)}
        />
      </TableCell>
      <TableCell>
        <span
          className="cursor-pointer select-none"
          onClick={() => onCheck(!checked)}
        >
          {name}
          {changed && (
            <em className={styles.changedBadge} aria-label={L.elements.unsavedChanges}>
              CHANGED
            </em>
          )}
        </span>
      </TableCell>
      <TableCell>
        <Dropdown
          options={roleOptions}
          value={role}
          onChange={onRoleChange}
          size="sm"
          ariaLabel={`${name} role`}
          className={cn(changed && "[&_button]:border-mint/45")}
        />
      </TableCell>
    </TableRow>
  );
};

export default MembershipRow;

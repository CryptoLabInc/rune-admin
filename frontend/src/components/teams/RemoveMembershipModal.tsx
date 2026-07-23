import Button from "@/components/elements/Button";
import Notice from "@/components/elements/Notice";
import ModalLayout from "@/components/layout/ModalLayout";
import { BTN_TEXT, MODAL_TITLES } from "@/constants/commonConstants";
import { L } from "@/locales";
import type { TTeamMemberRole } from "@/types/teamTypes";

export interface TMembershipRemoval {
  account: string;
  role: TTeamMemberRole;
}

interface RemoveMembershipModalProps {
  teamName: string;
  members: TMembershipRemoval[];
  onClose: () => void;
  onConfirm: () => void;
}

const styles = {
  table: "w-full border-collapse text-sm",
  th: "border-border text-faint border px-3 py-1.5 text-left font-mono text-tag font-medium tracking-[0.08em]",
  td: "border-border text-muted-foreground border px-3 py-1.5",
};

/**
 * RemoveMembershipModal is the 멤버십 제거 confirmation (SC-14): lists
 * exactly the memberships being removed (account · team · role) — only
 * what is listed is removed, no sub-team cascade (C10). Mount
 * conditionally.
 */
const RemoveMembershipModal = ({
  teamName,
  members,
  onClose,
  onConfirm,
}: RemoveMembershipModalProps) => {
  return (
    <ModalLayout title={MODAL_TITLES.removeMembership} isOpen>
      <div className="flex w-full flex-col gap-4">
        <p className="text-base">{L.teams.removeIntro}</p>
        <table className={styles.table}>
          <thead>
            <tr>
              <th className={styles.th}>{L.common.memberName}</th>
              <th className={styles.th}>{L.common.team}</th>
              <th className={styles.th}>{L.common.role}</th>
            </tr>
          </thead>
          <tbody>
            {members.map((member) => (
              <tr key={member.account}>
                <td className={styles.td}>{member.account}</td>
                <td className={styles.td}>{teamName}</td>
                <td className={styles.td}>{member.role}</td>
              </tr>
            ))}
          </tbody>
        </table>
        <Notice tone="info">{L.teams.removeKeepSubteams}</Notice>
      </div>
      <div className="flex w-full gap-2">
        <Button
          btnText={BTN_TEXT.cancel}
          btnSize="md"
          btnColor="grayOutline"
          handleClick={onClose}
        />
        <Button
          btnText={BTN_TEXT.remove}
          btnSize="md"
          btnColor="redFilled"
          handleClick={onConfirm}
        />
      </div>
    </ModalLayout>
  );
};

export default RemoveMembershipModal;

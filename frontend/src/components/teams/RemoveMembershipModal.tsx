import Button from "@/components/elements/Button";
import Notice from "@/components/elements/Notice";
import ModalLayout from "@/components/layout/ModalLayout";
import { BTN_TEXT } from "@/constants/commonConstants";
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
    <ModalLayout title="멤버십 제거" isOpen>
      <div className="flex w-full flex-col gap-4">
        <p className="text-base">다음 멤버십을 제거합니다:</p>
        <table className={styles.table}>
          <thead>
            <tr>
              <th className={styles.th}>account</th>
              <th className={styles.th}>팀</th>
              <th className={styles.th}>role</th>
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
        <Notice tone="info">
          하위 팀 소속은 유지됩니다. 필요할 경우 개별 선택 후 제거하세요.
        </Notice>
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

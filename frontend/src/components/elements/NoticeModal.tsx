import Button from "@/components/elements/Button";
import ModalLayout from "@/components/layout/ModalLayout";
import { useNoticeStore } from "@/stores/noticeStore";
import { cn } from "@/utils/cn";

/**
 * NoticeModal is the shared blocking result-notice modal. The title names
 * the attempted action; success/failure is conveyed by the message text
 * only (error tone renders it in the negative color). Closes solely via
 * [확인], which also runs the notice's onConfirm follow-up. Fire notices
 * with useNoticeStore().showNotice; mounted once in App.
 */
const NoticeModal = () => {
  const { notice, dismissNotice } = useNoticeStore();
  if (!notice) return null;
  return (
    <ModalLayout title={notice.title} isOpen>
      <p
        className={cn(
          "text-center text-base",
          notice.tone === "error" && "text-negative",
        )}
      >
        {notice.message}
      </p>
      <Button
        btnText="확인"
        btnSize="md"
        btnColor="grayOutline"
        handleClick={dismissNotice}
      />
    </ModalLayout>
  );
};

export default NoticeModal;

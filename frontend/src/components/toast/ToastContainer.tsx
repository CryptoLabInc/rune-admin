import { createPortal } from "react-dom";

import { cn } from "@/utils/cn";
import { useToastStore } from "@/stores/toastStore";

const styles = {
  stack: "fixed top-4 right-4 z-90 flex flex-col items-end gap-2",
  toast:
    "bg-panel-solid animate-toast-in flex cursor-pointer items-center gap-2.5 rounded-md border px-4 py-3 text-sm shadow-[0_24px_48px_-20px_rgba(0,0,0,0.65)] hover:border-border-strong motion-reduce:animate-none",
  toastLeaving: "animate-toast-out",
  icon: "text-tag grid size-[18px] flex-none place-items-center rounded-full border border-current font-mono",
};

const TONE_ICON = {
  info: { glyph: "i", color: "text-accent-blue" },
  success: { glyph: "✓", color: "text-mint" },
  error: { glyph: "!", color: "text-negative" },
} as const;

/**
 * ToastContainer renders the top-right toast queue (wireframe spec:
 * slides in right-to-left, holds 2s, exits to the right). Mounted once
 * in App; fire toasts via useToastStore().showToast.
 */
const ToastContainer = () => {
  const toasts = useToastStore((state) => state.toasts);
  const dismissToast = useToastStore((state) => state.dismissToast);
  if (toasts.length === 0) return null;
  return createPortal(
    <div className={styles.stack}>
      {toasts.map((toast) => (
        <div
          key={toast.id}
          role={toast.tone === "error" ? "alert" : "status"}
          className={cn(styles.toast, toast.leaving && styles.toastLeaving)}
          title="클릭하여 닫기"
          onClick={() => dismissToast(toast.id)}
        >
          <span
            aria-hidden="true"
            className={cn(styles.icon, TONE_ICON[toast.tone].color)}
          >
            {TONE_ICON[toast.tone].glyph}
          </span>
          {toast.message}
        </div>
      ))}
    </div>,
    document.body,
  );
};

export default ToastContainer;

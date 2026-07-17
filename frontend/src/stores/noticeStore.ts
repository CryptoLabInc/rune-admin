import { create } from "zustand";

import type { TToastTone } from "@/types/commonTypes";

/** Notice tone reuses the toast tone scale; drives content styling only. */
export type TNoticeTone = TToastTone;

/** A blocking result notice; one is shown at a time. */
export interface TNotice {
  title: string;
  message: string;
  tone: TNoticeTone;
  /** Follow-up action run when the user dismisses via [확인]. */
  onConfirm?: () => void;
}

interface NoticeState {
  notice: TNotice | null;
  /** Show a notice modal; replaces any currently shown notice. */
  showNotice: (
    title: string,
    message: string,
    tone?: TNoticeTone,
    onConfirm?: () => void,
  ) => void;
  /** Run the notice's onConfirm (if any), then close it. */
  dismissNotice: () => void;
}

/** useNoticeStore holds the single blocking result-notice modal. */
export const useNoticeStore = create<NoticeState>((set, get) => ({
  notice: null,
  showNotice: (title, message, tone = "info", onConfirm) => {
    set({ notice: { title, message, tone, onConfirm } });
  },
  dismissNotice: () => {
    const current = get().notice;
    if (!current) return;
    set({ notice: null });
    current.onConfirm?.();
  },
}));

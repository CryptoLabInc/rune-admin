import { create } from "zustand";

import type { TToastTone } from "@/types/commonTypes";

/** Wireframe toast timing: hold 2s, then the 0.3s exit animation. */
const TOAST_HOLD_MS = 2000;
const TOAST_EXIT_MS = 300;

/** A queued toast; `leaving` drives the exit animation before removal. */
export interface TToastItem {
  id: number;
  message: string;
  tone: TToastTone;
  leaving: boolean;
}

interface ToastState {
  toasts: TToastItem[];
  /** Enqueue a toast; it dismisses itself after the wireframe timings. */
  showToast: (message: string, tone?: TToastTone) => void;
  /** Start the exit animation for one toast. */
  dismissToast: (id: number) => void;
  /** Drop the toast from the queue (after the exit animation). */
  removeToast: (id: number) => void;
}

let nextToastId = 0;

/** useToastStore holds the top-right toast queue (wireframe spec). */
export const useToastStore = create<ToastState>((set, get) => ({
  toasts: [],
  showToast: (message, tone = "info") => {
    nextToastId += 1;
    const id = nextToastId;
    set((state) => ({
      toasts: [...state.toasts, { id, message, tone, leaving: false }],
    }));
    setTimeout(() => get().dismissToast(id), TOAST_HOLD_MS);
  },
  dismissToast: (id) => {
    /* Already leaving (e.g. clicked right before the auto-dismiss timer
       fires) — don't restart the exit sequence. */
    const target = get().toasts.find((toast) => toast.id === id);
    if (!target || target.leaving) return;
    set((state) => ({
      toasts: state.toasts.map((toast) =>
        toast.id === id ? { ...toast, leaving: true } : toast,
      ),
    }));
    setTimeout(() => get().removeToast(id), TOAST_EXIT_MS);
  },
  removeToast: (id) => {
    set((state) => ({
      toasts: state.toasts.filter((toast) => toast.id !== id),
    }));
  },
}));

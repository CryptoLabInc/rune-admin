import { create } from "zustand";

import { type TAlert } from "@/types/commonTypes";

type ExtendedTAlert = TAlert & { isOpen: boolean };

interface AlertStoreProps {
  alert: ExtendedTAlert;
  setAlert: (target: TAlert) => void;
  closeAlert: () => void;
}

const initialAlert: ExtendedTAlert = { title: "", content: "", isOpen: false };

/** useAlertStore drives the global alert modal (wireframe alert pattern). */
export const useAlertStore = create<AlertStoreProps>((set) => ({
  alert: initialAlert,
  setAlert: (target: TAlert) =>
    set((state) => ({ ...state, alert: { ...target, isOpen: true } })),
  closeAlert: () => set((state) => ({ ...state, alert: initialAlert })),
}));

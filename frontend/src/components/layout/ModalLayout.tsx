import { useEffect, type ReactNode } from "react";
import { createPortal } from "react-dom";

import { cn } from "@/utils/cn";

interface ModalLayoutProps {
  title: string;
  isOpen: boolean;
  isWide?: boolean;
  children: ReactNode;
}

/**
 * ModalLayout is the shared modal shell (adopted from envector-cloud):
 * centered title on top, content below, and full-width bottom buttons —
 * all passed as children after the title. The scrim never closes the
 * modal; modals close only via their [닫기] button (wireframe common
 * modal rule). Sizing: 500px default / 640px wide, min-height 280px.
 * Layout only — visual design tokens land later.
 */
const ModalLayout = ({
  title,
  isOpen,
  isWide = false,
  children,
}: ModalLayoutProps) => {
  /* Disable background scroll & prevent layout shift while open.
     Guarded on isOpen: the effect also runs when the component stays
     mounted with isOpen=false, which would lock the page silently. */
  useEffect(() => {
    if (!isOpen) return;
    const scrollY = window.scrollY;
    document.body.style.position = "fixed";
    document.body.style.top = `-${scrollY}px`;
    document.body.style.width = "100%";
    document.body.style.overflowY = "scroll";
    document.body.setAttribute("data-scroll-lock", `${scrollY}`);

    return () => {
      document.body.style.position = "";
      document.body.style.top = "";
      document.body.style.width = "";
      document.body.style.overflowY = "";
      const scrollYAttr = document.body.getAttribute("data-scroll-lock");
      if (scrollYAttr) {
        window.scrollTo(0, parseInt(scrollYAttr));
        document.body.removeAttribute("data-scroll-lock");
      }
    };
  }, [isOpen]);

  return createPortal(
    <div className="fixed inset-0 z-80 flex items-center justify-center">
      <div className="bg-scrim/60 pointer-events-none fixed inset-0 backdrop-blur-[2px]" />
      {/* Modal container */}
      <div
        className={cn(
          "bg-panel-solid border-border relative flex h-fit max-h-[80vh] min-h-[280px] flex-col items-center gap-8 overflow-y-auto rounded-xl border p-8 shadow-[0_24px_48px_-20px_rgba(0,0,0,0.65)]",
          isWide ? "w-[640px]" : "w-[500px]",
        )}
      >
        <h2 className="w-full text-center text-xl font-semibold">{title}</h2>
        <div className="flex w-full grow flex-col justify-between gap-8">
          {children}
        </div>
      </div>
    </div>,
    document.body,
  );
};

export default ModalLayout;

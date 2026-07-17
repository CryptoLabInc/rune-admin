import { useEffect, useId, type ReactNode } from "react";
import { createPortal } from "react-dom";

const styles = {
  scrim: "bg-scrim/60 fixed inset-0 z-80 flex justify-end backdrop-blur-[6px]",
  panel:
    "border-muted-foreground/26 animate-drawer-slide flex h-full w-[472px] flex-col border-l bg-[color:color-mix(in_srgb,var(--color-panel-solid)_60%,var(--color-well))] shadow-[-24px_0_70px_rgba(0,0,0,0.38)] motion-reduce:animate-none",
  header: "relative border-b px-[22px] pt-[22px] pb-[18px]",
  eyebrow: "text-tag text-mint m-0 mb-2 font-mono tracking-[0.11em]",
  title: "text-xl font-semibold",
  subtitle: "text-faint mt-2 block font-mono text-xs",
  body: "flex-1 overflow-auto px-[22px] py-5",
  footer:
    "bg-muted-foreground/[2%] grid grid-cols-[minmax(0,92px)_minmax(0,1fr)] gap-2 border-t px-[22px] py-[14px]",
};

interface DrawerLayoutProps {
  isOpen: boolean;
  title: string;
  eyebrow?: string;
  subtitle?: string;
  onClose: () => void;
  footer?: ReactNode;
  children: ReactNode;
}

/**
 * DrawerLayout is the right-side slide-over panel (SC-13 member
 * detail; frame from UIKIT AdminDrawer). It closes via a scrim click
 * or footer buttons — there is no ✕ button (revised 2026-07-13;
 * unlike modals, which close via their [닫기] button only).
 */
const DrawerLayout = ({
  isOpen,
  title,
  eyebrow,
  subtitle,
  onClose,
  footer,
  children,
}: DrawerLayoutProps) => {
  const titleId = useId();

  /* Disable background scroll & prevent layout shift while open.
     Guarded on isOpen: the drawer stays mounted while closed (isOpen
     toggles rendering below), so an unguarded effect locks the page. */
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

  if (!isOpen) return null;
  return createPortal(
    <div
      className={styles.scrim}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <aside
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className={styles.panel}
      >
        <header className={styles.header}>
          {eyebrow && <p className={styles.eyebrow}>{eyebrow}</p>}
          <h2 id={titleId} className={styles.title}>
            {title}
          </h2>
          {subtitle && <span className={styles.subtitle}>{subtitle}</span>}
        </header>
        <div className={styles.body}>{children}</div>
        {footer && <footer className={styles.footer}>{footer}</footer>}
      </aside>
    </div>,
    document.body,
  );
};

export default DrawerLayout;

import { useEffect, useRef, useState } from "react";

import Button from "@/components/elements/Button";
import { BTN_TEXT } from "@/constants/commonConstants";

interface ProfileMenuProps {
  // avatar is optional: the backend omits it when the principal has no picture,
  // and the button falls back to AvatarFallback on a missing/failed image.
  me: { email: string; avatar?: string };
  // plan is the lowercase wire string from the session (e.g. "free"); shown
  // capitalized. Empty/missing falls back to "Free".
  plan?: string;
  onSignOut: () => void;
}

/** planLabel capitalizes the wire plan for display, defaulting to "Free". */
const planLabel = (plan?: string): string =>
  plan ? plan.charAt(0).toUpperCase() + plan.slice(1) : "Free";

/** AvatarFallback is the neutral user glyph shown when the OAuth avatar
    image fails to load (SC-03 element 3 — image load failure → default icon). */
const AvatarFallback = () => (
  <span
    data-testid="avatar-fallback"
    className="bg-muted-foreground/20 text-muted-foreground grid size-8 place-items-center rounded-full"
    aria-hidden="true"
  >
    <svg viewBox="0 0 16 16" fill="currentColor" className="size-4">
      <circle cx="8" cy="5" r="3" />
      <path d="M2.5 14a5.5 5.5 0 0 1 11 0z" />
    </svg>
  </span>
);

/**
 * ProfileMenu is the SC-03 navbar profile control: an avatar button that
 * toggles a popover showing the account, plan (from the session), and a
 * Sign out action. The popover overlays (position:absolute, no layout shift)
 * and closes on outside click or Escape. Rendered only when logged in.
 */
const ProfileMenu = ({ me, plan, onSignOut }: ProfileMenuProps) => {
  const [open, setOpen] = useState(false);
  const [imgError, setImgError] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onMouseDown = (e: MouseEvent) => {
      if (!containerRef.current?.contains(e.target as Node)) setOpen(false);
    };
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onMouseDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onMouseDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  return (
    <div ref={containerRef} className="relative">
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="프로필 메뉴"
        className="block cursor-pointer rounded-full"
        onClick={() => setOpen((v) => !v)}
      >
        {imgError || !me.avatar ? (
          <AvatarFallback />
        ) : (
          <img
            src={me.avatar}
            alt="프로필 이미지"
            className="size-8 rounded-full object-cover"
            onError={() => setImgError(true)}
          />
        )}
      </button>

      {open && (
        <div
          role="menu"
          className="border-border bg-background absolute top-full right-0 z-20 mt-2 flex w-52 flex-col gap-3 rounded-md border p-3 shadow-md"
        >
          <div className="text-foreground text-md truncate font-medium">
            {me.email}
          </div>
          <div className="text-muted-foreground text-xs">
            플랜: {planLabel(plan)}
          </div>
          <hr className="border-border my-2" />
          <Button
            btnText={BTN_TEXT.signOut}
            btnSize="sm"
            btnColor="grayOutline"
            handleClick={onSignOut}
          />
        </div>
      )}
    </div>
  );
};

export default ProfileMenu;

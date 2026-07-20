import { useEffect, useRef, useState } from "react";

import TextButton from "@/components/elements/TextButton";
import { BTN_TEXT } from "@/constants/commonConstants";

interface ProfileMenuProps {
  // avatar is optional: the backend omits it when the principal has no picture,
  // and the button falls back to AvatarFallback on a missing/failed image.
  me: { email: string; avatar?: string };
  onSignOut: () => void;
}

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
 * toggles a popover showing the account, plan (static "Free" — display-only
 * until the session API carries the account plan), and a Sign out action. The
 * popover overlays (position:absolute, no layout shift) and closes on outside
 * click or Escape. Rendered only when logged in.
 */
const ProfileMenu = ({ me, onSignOut }: ProfileMenuProps) => {
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
          className="border-border bg-background absolute top-full right-0 z-20 mt-2 flex w-60 flex-col gap-3 rounded-md border p-3 shadow-md"
        >
          <div className="text-foreground text-md truncate font-medium">
            {me.email}
          </div>
          <div className="text-subtle text-sm">플랜: Free</div>
          <hr className="border-border my-2" />
          <TextButton
            btnText={BTN_TEXT.signOut}
            className="self-end"
            tone="gray"
            handleClick={onSignOut}
          />
        </div>
      )}
    </div>
  );
};

export default ProfileMenu;

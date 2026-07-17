/**
 * IconInfo is the information glyph (dot over stem) — Notice info tone.
 * Inherits color via currentColor.
 */
const IconInfo = ({ className }: { className?: string }) => (
  <svg
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.6}
    strokeLinecap="round"
    className={className}
    aria-hidden="true"
  >
    <path d="M8 7.2v5.2" />
    <circle cx="8" cy="3.8" r="0.9" fill="currentColor" stroke="none" />
  </svg>
);

export default IconInfo;

/**
 * IconAlert is the exclamation glyph (stem over dot) — Feedback error
 * state, Notice error tone. Inherits color via currentColor.
 */
const IconAlert = ({ className }: { className?: string }) => (
  <svg
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.6}
    strokeLinecap="round"
    className={className}
    aria-hidden="true"
  >
    <path d="M8 3.6v5.2" />
    <circle cx="8" cy="12.2" r="0.9" fill="currentColor" stroke="none" />
  </svg>
);

export default IconAlert;

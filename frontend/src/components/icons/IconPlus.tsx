/**
 * IconPlus is the expand/create glyph — team-tree closed toggle,
 * Feedback empty state. Inherits color via currentColor.
 */
const IconPlus = ({ className }: { className?: string }) => (
  <svg
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.6}
    strokeLinecap="round"
    className={className}
    aria-hidden="true"
  >
    <path d="M8 2.5v11M2.5 8h11" />
  </svg>
);

export default IconPlus;

/**
 * IconMinus is the collapse glyph — team-tree open toggle. Inherits
 * color via currentColor.
 */
const IconMinus = ({ className }: { className?: string }) => (
  <svg
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.6}
    strokeLinecap="round"
    className={className}
    aria-hidden="true"
  >
    <path d="M2.5 8h11" />
  </svg>
);

export default IconMinus;

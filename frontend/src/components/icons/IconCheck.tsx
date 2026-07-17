/**
 * IconCheck is the success glyph — Notice success tone. Inherits color
 * via currentColor.
 */
const IconCheck = ({ className }: { className?: string }) => (
  <svg
    viewBox="0 0 16 16"
    fill="none"
    stroke="currentColor"
    strokeWidth={1.6}
    strokeLinecap="round"
    strokeLinejoin="round"
    className={className}
    aria-hidden="true"
  >
    <path d="m3 8.5 3.2 3.2L13 5" />
  </svg>
);

export default IconCheck;

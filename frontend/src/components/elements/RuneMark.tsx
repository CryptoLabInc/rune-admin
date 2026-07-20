import { useId } from "react";

interface RuneMarkProps {
  /** Rendered size in px (the mark is square-ish, 490×520). Defaults to 26. */
  size?: number;
  className?: string;
}

/**
 * RuneMark is the Rune brand symbol (color variant), ported from
 * UIKIT/modules/rune-brand/rune-mark.svg. It renders inline SVG so the three
 * brand gradients (mint / navy / teal) survive without an asset pipeline.
 *
 * The three gradients use userSpaceOnUse coordinates, so their ids must be
 * unique per instance — otherwise a second mark on the page would reference
 * the first one's defs. useId() gives each render its own id namespace.
 *
 * It is decorative: the adjacent "Rune Console" wordmark carries the accessible
 * name, so the mark is hidden from assistive tech (aria-hidden).
 */
const RuneMark = ({ size = 26, className }: RuneMarkProps) => {
  const id = useId();
  const navy = `${id}-navy`;
  const mint = `${id}-mint`;
  const teal = `${id}-teal`;

  return (
    <svg
      width={size}
      height={(size * 520) / 490}
      viewBox="0 0 490 520"
      fill="none"
      xmlns="http://www.w3.org/2000/svg"
      aria-hidden="true"
      focusable="false"
      className={className}
    >
      <defs>
        <linearGradient
          id={navy}
          x1="110"
          y1="116"
          x2="230"
          y2="395"
          gradientUnits="userSpaceOnUse"
        >
          <stop offset="0" stopColor="#7ff5dd" />
          <stop offset="1" stopColor="#29bda7" />
        </linearGradient>
        <linearGradient
          id={mint}
          x1="260"
          y1="72"
          x2="430"
          y2="320"
          gradientUnits="userSpaceOnUse"
        >
          <stop offset="0" stopColor="#ccfff3" />
          <stop offset="1" stopColor="#5cffd6" />
        </linearGradient>
        <linearGradient
          id={teal}
          x1="342"
          y1="294"
          x2="292"
          y2="468"
          gradientUnits="userSpaceOnUse"
        >
          <stop offset="0" stopColor="#2ac6a8" />
          <stop offset="1" stopColor="#189e86" />
        </linearGradient>
      </defs>
      <path d="M365 281 L440 341 L259 475 L259 352 Z" fill={`url(#${teal})`} />
      <path
        d="M257 64 L440 201 L440 341 L365 281 L258 201 Z"
        fill={`url(#${mint})`}
      />
      <path
        d="M257 64 L95 185 L95 351 L164 401 L265 313 L190 260 L258 201 Z"
        fill={`url(#${navy})`}
      />
      <path
        d="M95 351 L258 201 L190 260 L265 313 L164 401 Z"
        fill="#178c7e"
        opacity="0.38"
      />
    </svg>
  );
};

export default RuneMark;

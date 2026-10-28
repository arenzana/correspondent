<!-- All caught up: the paper read, a cup of coffee still steaming. Drawn
     once here and in apple/App/Sources/Views/CaughtUp.swift; keep the two
     the same. -->
<svg class="caught-up" viewBox="0 30 160 80" aria-hidden="true">
  <g fill="none" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.6">
    <path class="table" d="M6 100.5 L154 100.5" stroke-width="1.5" />
    <path class="line" d="M16 63 L78 57 L82 99 L20 100.5 Z" fill="var(--sheet)" />
    <path class="fold" d="M48.5 60 L51.5 99.8" stroke-width="1.2" />
    <path class="accent" d="M24 68 L56 64.9" stroke-width="3" />
    <path class="text" stroke-width="1.4"
      d="M25 75 L43 73.3 M25.5 81 L43.5 79.3 M26 87 L44 85.3 M26.5 93 L44.5 91.3 M56 71.3 L73 69.7 M56.5 77.3 L73.5 75.7 M57 83.3 L74 81.7 M57.5 89.3 L74.5 87.7 M58 95 L68 94" />
    <path class="accent" d="M86 57 C86 61.4 82.4 65 78 65 C73.6 65 70 61.4 70 57 C70 52.6 73.6 49 78 49 C82.4 49 86 52.6 86 57 Z" fill="var(--sheet)" />
    <path class="accent" d="M74.5 57.3 L77 59.8 L81.8 54.6" stroke-width="1.8" />
    <path class="line" d="M90 99 L150 99 Q146 104.5 120 104.5 Q94 104.5 90 99 Z" fill="var(--fog)" />
    <path class="line" d="M141 76 C153 73 155 90 138 90" />
    <path class="line" d="M98 70 L142 70 Q141 96 120 97.5 Q99 96 98 70 Z" fill="var(--sheet)" />
    <path class="line" d="M98 70 Q120 63 142 70 Q120 77 98 70 Z" fill="var(--seal)" />
    <path class="steam" d="M111 58 C107 52 115 48 111 41" stroke-width="1.5" />
    <path class="steam s2" d="M120.5 56 C116.5 49 124.5 45 120.5 36" stroke-width="1.5" />
    <path class="steam s3" d="M130 58 C126 52 134 48 130 41" stroke-width="1.5" />
  </g>
</svg>

<style>
  .caught-up {
    display: block;
    width: min(200px, 60vw);
    height: auto;
    margin: 0 0 20px -6px;
  }
  .table,
  .fold {
    stroke: var(--rule);
  }
  .line {
    stroke: var(--ink);
  }
  .accent {
    stroke: var(--accent);
  }
  .text {
    stroke: var(--ring);
  }
  .steam {
    stroke: var(--slate);
    opacity: 0.7;
    animation: rise 2.8s ease-in-out infinite alternate;
  }
  .s2 {
    animation-delay: -0.9s;
  }
  .s3 {
    animation-delay: -1.8s;
  }
  @keyframes rise {
    from {
      transform: translateY(1px);
      opacity: 0.75;
    }
    to {
      transform: translateY(-3px);
      opacity: 0.3;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    .steam {
      animation: none;
    }
  }
</style>

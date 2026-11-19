import { chartPath } from "../lib/chart";

export function Sparkline({ values }: { values: number[] }) {
  const path = chartPath(values.map((skill, t) => ({ t, skill })), 120, 32);
  return (
    <svg className="spark" viewBox="0 0 120 32" aria-hidden="true">
      {path ? <path d={path} /> : <line x1={0} y1={31} x2={120} y2={31} className="spark-empty" />}
    </svg>
  );
}

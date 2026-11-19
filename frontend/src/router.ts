import { useEffect, useState } from "react";

export type Route =
  | { name: "home" }
  | { name: "quiz"; id: number }
  | { name: "topic"; slug: string }
  | { name: "settings" };

export function parseRoute(hash: string): Route {
  const [head, arg] = hash.replace(/^#\/?/, "").split("/").filter(Boolean);
  if (head === "quiz" && arg && /^\d+$/.test(arg)) return { name: "quiz", id: Number(arg) };
  if (head === "topic" && arg) return { name: "topic", slug: decodeURIComponent(arg) };
  if (head === "settings") return { name: "settings" };
  return { name: "home" };
}

export function useRoute(): Route {
  const [route, setRoute] = useState(() => parseRoute(window.location.hash));
  useEffect(() => {
    const onChange = () => setRoute(parseRoute(window.location.hash));
    window.addEventListener("hashchange", onChange);
    return () => window.removeEventListener("hashchange", onChange);
  }, []);
  return route;
}

export function go(hash: string): void {
  window.location.hash = hash;
}

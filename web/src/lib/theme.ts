export type Theme = "light" | "dark";
export type Preference = Theme | "system";

const KEY = "balise-theme";

export function resolveTheme(stored: string | null, systemPrefersDark: boolean): Theme {
  if (stored === "light" || stored === "dark") return stored;
  return systemPrefersDark ? "dark" : "light";
}

export function readPreference(): Preference {
  try {
    const stored = localStorage.getItem(KEY);
    return stored === "light" || stored === "dark" ? stored : "system";
  } catch {
    return "system";
  }
}

export function applyTheme(preference: Preference): Theme {
  const systemPrefersDark = window.matchMedia("(prefers-color-scheme: dark)").matches;
  const theme = resolveTheme(preference === "system" ? null : preference, systemPrefersDark);
  if (preference === "system") {
    document.documentElement.removeAttribute("data-theme");
  } else {
    document.documentElement.setAttribute("data-theme", preference);
  }
  try {
    if (preference === "system") localStorage.removeItem(KEY);
    else localStorage.setItem(KEY, preference);
  } catch {
    /* private browsing — the attribute still applies for this session */
  }
  return theme;
}

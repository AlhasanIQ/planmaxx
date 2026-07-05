export type ThemeMode = "system" | "dark" | "light";
export type ResolvedTheme = "dark" | "light";

const storageKey = "planmaxx.theme";

export function resolveThemeMode(mode: ThemeMode, prefersDark: boolean): ResolvedTheme {
  if (mode === "system") {
    return prefersDark ? "dark" : "light";
  }
  return mode;
}

export function nextThemeMode(mode: ThemeMode): ThemeMode {
  if (mode === "system") return "dark";
  if (mode === "dark") return "light";
  return "system";
}

export function sanitizeThemeMode(value: string | null): ThemeMode {
  if (value === "dark" || value === "light" || value === "system") {
    return value;
  }
  return "system";
}

export function readThemeModeFromStorage(storage: Pick<Storage, "getItem"> | null | undefined): ThemeMode {
  if (!storage) {
    return "system";
  }
  try {
    return sanitizeThemeMode(storage.getItem(storageKey));
  } catch {
    return "system";
  }
}

export function writeThemeModeToStorage(storage: Pick<Storage, "setItem"> | null | undefined, mode: ThemeMode) {
  try {
    storage?.setItem(storageKey, mode);
  } catch {
    // Storage can be unavailable in restrictive browser contexts.
  }
}

export function prefersDarkFromMatcher(
  matchMedia: ((query: string) => Pick<MediaQueryList, "matches">) | null | undefined,
): boolean {
  try {
    return Boolean(matchMedia?.("(prefers-color-scheme: dark)").matches);
  } catch {
    return false;
  }
}

export type ColorSchemeMediaQuery = Pick<MediaQueryList, "matches"> & {
  addEventListener?: (type: "change", listener: () => void) => void;
  removeEventListener?: (type: "change", listener: () => void) => void;
  addListener?: (listener: () => void) => void;
  removeListener?: (listener: () => void) => void;
};

export function subscribeToColorSchemeChanges(
  media: ColorSchemeMediaQuery | null | undefined,
  callback: () => void,
): () => void {
  if (!media) {
    return noop;
  }

  const listener = () => callback();
  if (media.addEventListener && media.removeEventListener) {
    try {
      media.addEventListener("change", listener);
      return () => {
        try {
          media.removeEventListener?.("change", listener);
        } catch {
          // Listener cleanup should not break the review UI.
        }
      };
    } catch {
      // Try the legacy API below.
    }
  }

  if (media.addListener && media.removeListener) {
    try {
      media.addListener(listener);
      return () => {
        try {
          media.removeListener?.(listener);
        } catch {
          // Listener cleanup should not break the review UI.
        }
      };
    } catch {
      return noop;
    }
  }

  return noop;
}

export function readStoredThemeMode(): ThemeMode {
  return readThemeModeFromStorage(browserStorage());
}

export function writeStoredThemeMode(mode: ThemeMode) {
  writeThemeModeToStorage(browserStorage(), mode);
}

function browserStorage(): Storage | null {
  if (typeof window === "undefined") {
    return null;
  }
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}

function noop() {}

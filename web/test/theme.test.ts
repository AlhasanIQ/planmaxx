import { describe, expect, test } from "bun:test";
import {
  nextThemeMode,
  prefersDarkFromMatcher,
  readThemeModeFromStorage,
  resolveThemeMode,
  sanitizeThemeMode,
  subscribeToColorSchemeChanges,
  writeThemeModeToStorage,
} from "../src/lib/theme";

describe("theme helpers", () => {
  test("resolves system mode from the browser preference", () => {
    expect(resolveThemeMode("system", true)).toBe("dark");
    expect(resolveThemeMode("system", false)).toBe("light");
  });

  test("resolves explicit modes without using the browser preference", () => {
    expect(resolveThemeMode("dark", false)).toBe("dark");
    expect(resolveThemeMode("light", true)).toBe("light");
  });

  test("cycles through persisted theme modes", () => {
    expect(nextThemeMode("system")).toBe("dark");
    expect(nextThemeMode("dark")).toBe("light");
    expect(nextThemeMode("light")).toBe("system");
  });

  test("sanitizes unknown persisted values to system", () => {
    expect(sanitizeThemeMode("dark")).toBe("dark");
    expect(sanitizeThemeMode("sepia")).toBe("system");
    expect(sanitizeThemeMode(null)).toBe("system");
  });

  test("falls back to system when storage reads are unavailable", () => {
    expect(readThemeModeFromStorage(blockedStorage())).toBe("system");
  });

  test("ignores unavailable storage writes", () => {
    expect(() => writeThemeModeToStorage(blockedStorage(), "dark")).not.toThrow();
  });

  test("reads dark preference from a matchMedia-compatible function", () => {
    expect(prefersDarkFromMatcher(() => ({ matches: true }))).toBe(true);
    expect(prefersDarkFromMatcher(() => ({ matches: false }))).toBe(false);
  });

  test("falls back to light preference when matchMedia is unavailable", () => {
    expect(prefersDarkFromMatcher(undefined)).toBe(false);
    expect(
      prefersDarkFromMatcher(() => {
        throw new Error("matchMedia blocked");
      }),
    ).toBe(false);
  });

  test("subscribes to modern color-scheme changes and unsubscribes cleanly", () => {
    let listener: (() => void) | null = null;
    let calls = 0;
    const unsubscribe = subscribeToColorSchemeChanges(
      {
        matches: false,
        addEventListener(type, nextListener) {
          expect(type).toBe("change");
          listener = nextListener;
        },
        removeEventListener(type, nextListener) {
          expect(type).toBe("change");
          if (listener === nextListener) listener = null;
        },
      },
      () => {
        calls++;
      },
    );

    listener?.();
    unsubscribe();
    listener?.();

    expect(calls).toBe(1);
    expect(listener).toBeNull();
  });

  test("falls back to legacy color-scheme listeners", () => {
    let listener: (() => void) | null = null;
    let calls = 0;
    const unsubscribe = subscribeToColorSchemeChanges(
      {
        matches: false,
        addListener(nextListener) {
          listener = nextListener;
        },
        removeListener(nextListener) {
          if (listener === nextListener) listener = null;
        },
      },
      () => {
        calls++;
      },
    );

    listener?.();
    unsubscribe();
    listener?.();

    expect(calls).toBe(1);
    expect(listener).toBeNull();
  });

  test("ignores unavailable color-scheme listener APIs", () => {
    expect(() =>
      subscribeToColorSchemeChanges(
        {
          matches: false,
          addEventListener() {
            throw new Error("listener blocked");
          },
          removeEventListener() {
            throw new Error("listener blocked");
          },
        },
        () => {},
      )(),
    ).not.toThrow();
  });
});

function blockedStorage(): Storage {
  return {
    get length() {
      throw new Error("storage blocked");
    },
    clear() {
      throw new Error("storage blocked");
    },
    getItem() {
      throw new Error("storage blocked");
    },
    key() {
      throw new Error("storage blocked");
    },
    removeItem() {
      throw new Error("storage blocked");
    },
    setItem() {
      throw new Error("storage blocked");
    },
  };
}

import { useCallback, useRef, useState } from "react";
import { api } from "../api";
import type { ChangeView } from "../types";

export function useRevisionComparison(onError: (message: string) => void) {
  const [diff, setDiff] = useState<ChangeView | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
	const requestSequence = useRef(0);

  const clear = useCallback(() => {
	requestSequence.current++;
    setDiff(null);
    setError(null);
	setLoading(false);
  }, []);

  const suppress = useCallback(() => {
	requestSequence.current++;
    setDiff(null);
    setError(null);
    setLoading(false);
  }, []);

  const reload = useCallback(async (from: string, to: string) => {
	setError(null);
	const request = ++requestSequence.current;
	setLoading(true);
	try {
	  const result = await api.revisionDiff(from, to);
	  if (request !== requestSequence.current) return;
	  setDiff(result);
	} catch (cause) {
	  if (request !== requestSequence.current) return;
      const message = cause instanceof Error ? cause.message : "Failed to load revision diff";
      setError(message);
      onError(message);
	} finally {
	  if (request === requestSequence.current) setLoading(false);
    }
  }, [onError]);

  const compare = useCallback(async (from: string, to: string) => {
    if (diff?.baseId === from && diff.targetId === to) {
      clear();
      return;
    }
	await reload(from, to);
  }, [clear, diff, reload]);

  return { diff, loading, error, compare, reload, clear, suppress };
}

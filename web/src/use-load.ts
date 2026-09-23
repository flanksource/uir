import { useEffect, useState } from "react";

export type Load<T> = { data?: T; error?: string; loading: boolean };

export function useLoad<T>(load: (() => Promise<T>) | null, key: string): Load<T> {
  const [result, setResult] = useState<Load<T> & { key: string }>({ key, loading: Boolean(load) });
  useEffect(() => {
    if (!load) {
      setResult({ key, loading: false });
      return;
    }
    let active = true;
    setResult({ key, loading: true });
    load().then((data) => { if (active) setResult({ key, data, loading: false }); }, (error: unknown) => {
      if (active) setResult({ key, error: String(error), loading: false });
    });
    return () => { active = false; };
    // key names every input that changes the request.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  return result.key === key ? result : { loading: Boolean(load) };
}

import { useCallback, useEffect, useState } from "react";

type State<T> = {
  data: T | undefined;
  error: string | undefined;
  loading: boolean;
};

/**
 * Runs a call and reports what happened.
 *
 * Every page needs the same three states - loading, failed, loaded - and a
 * page that only renders the last one shows an empty screen when a service is
 * down, which looks like "there is nothing" rather than "something is wrong".
 */
export function useAsync<T>(call: () => Promise<T>, deps: unknown[] = []) {
  const [state, setState] = useState<State<T>>({
    data: undefined,
    error: undefined,
    loading: true,
  });

  // eslint-disable-next-line react-hooks/exhaustive-deps
  const run = useCallback(call, deps);

  const reload = useCallback(() => {
    let cancelled = false;
    setState((previous) => ({ ...previous, loading: true, error: undefined }));

    run()
      .then((data) => {
        // The page may have been left while the call was in flight.
        if (!cancelled) {
          setState({ data, error: undefined, loading: false });
        }
      })
      .catch((error: unknown) => {
        if (!cancelled) {
          setState({
            data: undefined,
            error: error instanceof Error ? error.message : String(error),
            loading: false,
          });
        }
      });

    return () => {
      cancelled = true;
    };
  }, [run]);

  useEffect(() => reload(), [reload]);

  return { ...state, reload };
}

import { useState, useEffect, useCallback } from 'react';
import { client } from './client';

type ClientKey = keyof typeof client;
type ClientMethod<T extends ClientKey> = typeof client[T];
type ClientReturn<T extends ClientKey> = Awaited<ReturnType<ClientMethod<T>>>;

// Simple hook for queries (GET requests)
export function useQuery<T extends ClientKey>(method: T) {
  const [data, setData] = useState<ClientReturn<T> | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState<Error | null>(null);

  const refetch = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const fn = client[method] as () => Promise<ClientReturn<T>>;
      const result = await fn();
      setData(result);
    } catch (e) {
      setError(e instanceof Error ? e : new Error(String(e)));
    } finally {
      setIsLoading(false);
    }
  }, [method]);

  useEffect(() => {
    refetch();
  }, [refetch]);

  return { data, isLoading, error, refetch };
}

// Simple hook for mutations (POST requests)
export function useMutation<T extends ClientKey>(method: T) {
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  const mutate = useCallback(
    async (params: Parameters<ClientMethod<T>>[0]): Promise<ClientReturn<T>> => {
      setIsLoading(true);
      setError(null);
      try {
        const fn = client[method] as (p: typeof params) => Promise<ClientReturn<T>>;
        const result = await fn(params);
        return result;
      } catch (e) {
        const err = e instanceof Error ? e : new Error(String(e));
        setError(err);
        throw err;
      } finally {
        setIsLoading(false);
      }
    },
    [method]
  );

  return { mutate, isLoading, error };
}

import { queryOptions, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useMemo } from "react";
import { api, UnauthorizedError, User } from "./api";

export type AuthState =
  | { status: "loading" }
  | { status: "signed-in"; user: User }
  | { status: "signed-out" };

export interface AuthContextValue {
  state: AuthState;
  refresh: () => Promise<void>;
}

export const meQueryOptions = queryOptions({
  queryKey: ["auth", "me"] as const,
  queryFn: () => api.me(),
  retry: (failureCount, err) => {
    if (err instanceof UnauthorizedError) return false;
    return failureCount < 1;
  },
  staleTime: 5 * 60 * 1000,
});

export function useAuth(): AuthContextValue {
  const query = useQuery(meQueryOptions);
  const queryClient = useQueryClient();

  const refresh = useCallback(async () => {
    await queryClient.invalidateQueries({ queryKey: meQueryOptions.queryKey });
  }, [queryClient]);

  const state = useMemo<AuthState>(() => {
    if (query.isPending) return { status: "loading" };
    if (query.data) return { status: "signed-in", user: query.data };
    return { status: "signed-out" };
  }, [query.isPending, query.data]);

  return { state, refresh };
}

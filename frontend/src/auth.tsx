import {
  createContext,
  ReactNode,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";
import { api, UnauthorizedError, User } from "./api";

export type AuthState =
  | { status: "loading" }
  | { status: "signed-in"; user: User }
  | { status: "signed-out" };

export interface AuthContextValue {
  state: AuthState;
  refresh: () => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>({ status: "loading" });

  const refresh = useCallback(async () => {
    try {
      const user = await api.me();
      setState({ status: "signed-in", user });
    } catch (err) {
      if (err instanceof UnauthorizedError) {
        setState({ status: "signed-out" });
        return;
      }
      setState({ status: "signed-out" });
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  return (
    <AuthContext.Provider value={{ state, refresh }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used inside <AuthProvider>");
  return ctx;
}

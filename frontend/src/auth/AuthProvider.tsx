import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createContext, useContext, type PropsWithChildren } from "react";
import { api } from "../api/client";
import type { User } from "../api/types";

type AuthValue = {
  user?: User;
  loading: boolean;
  login(email: string, password: string, remember: boolean): Promise<void>;
  logout(): Promise<void>;
};

const AuthContext = createContext<AuthValue | null>(null);

export function AuthProvider({ children }: PropsWithChildren) {
  const queryClient = useQueryClient();
  const me = useQuery({
    queryKey: ["me"],
    queryFn: () => api.request<User>("/api/v1/auth/me"),
    retry: false,
  });

  const value: AuthValue = {
    user: me.data,
    loading: me.isLoading,
    async login(email, password, remember) {
      const result = await api.request<{ user: User; csrf_token: string }>("/api/v1/auth/login", {
        method: "POST",
        body: JSON.stringify({ email, password, remember }),
      }, false);
      api.setCSRF(result.csrf_token);
      queryClient.setQueryData(["me"], result.user);
    },
    async logout() {
      await api.request("/api/v1/auth/logout", { method: "POST", body: "{}" });
      queryClient.setQueryData(["me"], undefined);
      queryClient.clear();
    },
  };
  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth() {
  const value = useContext(AuthContext);
  if (!value) throw new Error("AuthProvider is missing");
  return value;
}

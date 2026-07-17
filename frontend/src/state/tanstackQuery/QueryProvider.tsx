import { type ReactNode } from "react";
import {
  MutationCache,
  QueryCache,
  QueryClient,
  QueryClientProvider,
} from "@tanstack/react-query";

import { handleAuthError } from "@/state/tanstackQuery/handleAuthError";

// A 401 from any query or mutation means the console session expired mid-use;
// handleAuthError redirects to sign-in (see handleAuthError.ts).
const queryClient = new QueryClient({
  queryCache: new QueryCache({ onError: handleAuthError }),
  mutationCache: new MutationCache({ onError: handleAuthError }),
});

interface QueryProviderProps {
  children: ReactNode;
}

/** QueryProvider supplies the app-wide TanStack Query client. */
const QueryProvider = ({ children }: QueryProviderProps) => {
  return (
    <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
  );
};

export default QueryProvider;

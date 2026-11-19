import { QueryCache, QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { isDown } from "./api/client";
import { App } from "./App";
import { ChatPageProvider } from "./chatContext";
import "./styles.css";

// Any request that finds the stack down rechecks /api/status, so the global
// banner appears even while the cached status still looks healthy.
const queryClient: QueryClient = new QueryClient({
  defaultOptions: { queries: { retry: 1, refetchOnWindowFocus: false } },
  queryCache: new QueryCache({
    onError: (err, query) => {
      if (isDown(err) && query.queryKey[0] !== "status") void queryClient.invalidateQueries({ queryKey: ["status"] });
    },
  }),
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ChatPageProvider>
        <App />
      </ChatPageProvider>
    </QueryClientProvider>
  </StrictMode>,
);

import React from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";
import "@flanksource/clicky-ui/styles.css";
import { App } from "./App";
import { ErrorWrapper } from "@flanksource/clicky-ui/components";
import { QueryClient, QueryClientProvider } from "@flanksource/clicky-ui/rpc";

const queryClient = new QueryClient();
createRoot(document.getElementById("root")!).render(<React.StrictMode><QueryClientProvider client={queryClient}><ErrorWrapper><App /></ErrorWrapper></QueryClientProvider></React.StrictMode>);

import React from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";
import "@flanksource/clicky-ui/styles.css";
import { App } from "./App";
import { setFallbackIconProvider } from "@flanksource/clicky-ui";
import { ErrorWrapper } from "@flanksource/clicky-ui/components";
import { FlanksourceIcon } from "@flanksource/clicky-ui/connections";
import { clickyIconProvider } from "@flanksource/clicky-ui/icons";
import { QueryClient, QueryClientProvider } from "@flanksource/clicky-ui/rpc";

// Resolve clicky-ui's name-based icons, such as the connection-type picker's, with @flanksource/icons
// behind clicky-ui's own generated set.
setFallbackIconProvider(clickyIconProvider(FlanksourceIcon));

const queryClient = new QueryClient();
createRoot(document.getElementById("root")!).render(<React.StrictMode><QueryClientProvider client={queryClient}><ErrorWrapper><App /></ErrorWrapper></QueryClientProvider></React.StrictMode>);

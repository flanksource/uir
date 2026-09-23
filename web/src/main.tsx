import React from "react";
import { createRoot } from "react-dom/client";
import "./styles.css";
import "@flanksource/clicky-ui/styles.css";
import { App } from "./App";
import { ErrorWrapper } from "@flanksource/clicky-ui/components";

createRoot(document.getElementById("root")!).render(<React.StrictMode><ErrorWrapper><App /></ErrorWrapper></React.StrictMode>);

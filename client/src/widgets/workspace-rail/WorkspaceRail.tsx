import type { Component } from "solid-js";
import { Avatar, IconButton } from "../../shared/ui";
import s from "./WorkspaceRail.module.css";

// Far-left vertical rail: workspace switcher + global actions (theme, user).
export const WorkspaceRail: Component<{
  workspaceName: string;
  userEmail: string;
  theme: "dark" | "light";
  onToggleTheme: () => void;
  onCreateWorkspace?: () => void;
}> = (props) => (
  <nav class={s.rail}>
    <Avatar name={props.workspaceName} size={40} />
    <IconButton label="Create workspace" onClick={() => props.onCreateWorkspace?.()}>
      +
    </IconButton>
    <div class={s.spacer} />
    <IconButton label="Toggle theme" onClick={() => props.onToggleTheme()}>
      {props.theme === "dark" ? "☀" : "☾"}
    </IconButton>
    <Avatar name={props.userEmail || "?"} size={32} />
  </nav>
);

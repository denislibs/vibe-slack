import type { Component } from "solid-js";
import { Input } from "../../shared/ui";
import s from "./Topbar.module.css";

// Full-width top bar: workspace name + (disabled) global search.
export const Topbar: Component<{ workspaceName: string }> = (props) => (
  <header class={s.topbar}>
    <span class={s.name}>{props.workspaceName}</span>
    <div class={s.search}>
      <Input value="" onInput={() => {}} placeholder="Search" aria-label="Search" />
    </div>
  </header>
);

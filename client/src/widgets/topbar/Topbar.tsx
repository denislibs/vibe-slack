import type { Component } from "solid-js";
import s from "./Topbar.module.css";

// Full-width top bar: history chevrons + (inert) global search + help.
export const Topbar: Component<{ workspaceName: string }> = (props) => (
  <header class={s.topbar}>
    <div class={s.nav}>
      <button type="button" class={s.chevron} aria-label="Go back">
        ‹
      </button>
      <button type="button" class={s.chevron} aria-label="Go forward">
        ›
      </button>
    </div>

    <input
      class={s.search}
      readOnly
      placeholder={`Search ${props.workspaceName}`}
      aria-label="Search"
    />

    <div class={s.right}>
      <button type="button" class={s.help} aria-label="Help">
        ?
      </button>
    </div>
  </header>
);

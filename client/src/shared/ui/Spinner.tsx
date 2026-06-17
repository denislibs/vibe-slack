import type { Component } from "solid-js";
import s from "./Spinner.module.css";

export const Spinner: Component = () => (
  <span class={s.spinner} role="status" aria-label="loading" />
);

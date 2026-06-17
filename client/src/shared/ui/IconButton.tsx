import type { Component, JSX } from "solid-js";
import s from "./IconButton.module.css";

export type IconButtonProps = {
  children: JSX.Element;
  label: string;
  onClick?: () => void;
  active?: boolean;
};

// Square icon-only button; uses its label as the accessible name.
export const IconButton: Component<IconButtonProps> = (props) => {
  return (
    <button
      type="button"
      aria-label={props.label}
      class={`${s.btn} ${props.active ? s.active : ""}`}
      onClick={() => props.onClick?.()}
    >
      {props.children}
    </button>
  );
};

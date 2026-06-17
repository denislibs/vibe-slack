import type { Component, JSX } from "solid-js";
import s from "./Button.module.css";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";

export type ButtonProps = {
  children: JSX.Element;
  onClick?: () => void;
  disabled?: boolean;
  type?: "button" | "submit";
  variant?: ButtonVariant;
};

// Pure presentational button. No business logic; ui-kit only.
export const Button: Component<ButtonProps> = (props) => {
  return (
    <button
      type={props.type ?? "button"}
      disabled={props.disabled}
      class={`${s.btn} ${s[props.variant ?? "secondary"]}`}
      onClick={() => props.onClick?.()}
    >
      {props.children}
    </button>
  );
};

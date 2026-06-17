import type { Component, JSX } from "solid-js";

export type ButtonProps = {
  children: JSX.Element;
  onClick?: () => void;
  disabled?: boolean;
  type?: "button" | "submit";
};

// Pure presentational button. No business logic; ui-kit only.
export const Button: Component<ButtonProps> = (props) => {
  return (
    <button type={props.type ?? "button"} disabled={props.disabled} onClick={() => props.onClick?.()}>
      {props.children}
    </button>
  );
};

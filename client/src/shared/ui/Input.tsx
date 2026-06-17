import type { Component, JSX } from "solid-js";
import s from "./Input.module.css";

export type InputProps = {
  value: string;
  onInput: (e: InputEvent & { currentTarget: HTMLInputElement }) => void;
  type?: string;
  placeholder?: string;
  "aria-label"?: string;
  onKeyDown?: (e: KeyboardEvent & { currentTarget: HTMLInputElement }) => void;
  autofocus?: boolean;
};

// Styled text input. Presentational; parent owns the value.
export const Input: Component<InputProps> = (props) => {
  return (
    <input
      class={s.input}
      type={props.type ?? "text"}
      value={props.value}
      placeholder={props.placeholder}
      aria-label={props["aria-label"]}
      autofocus={props.autofocus}
      onInput={props.onInput as JSX.EventHandler<HTMLInputElement, InputEvent>}
      onKeyDown={props.onKeyDown as JSX.EventHandler<HTMLInputElement, KeyboardEvent> | undefined}
    />
  );
};

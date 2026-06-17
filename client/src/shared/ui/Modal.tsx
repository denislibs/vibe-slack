import { Show } from "solid-js";
import type { Component, JSX } from "solid-js";
import s from "./Modal.module.css";

export type ModalProps = {
  open: boolean;
  onClose: () => void;
  children: JSX.Element;
};

// Backdrop + centered panel. Clicking the backdrop (not the panel) closes.
export const Modal: Component<ModalProps> = (props) => {
  return (
    <Show when={props.open}>
      <div data-testid="modal-backdrop" class={s.backdrop} onClick={() => props.onClose()}>
        <div class={s.panel} onClick={(e) => e.stopPropagation()}>
          {props.children}
        </div>
      </div>
    </Show>
  );
};

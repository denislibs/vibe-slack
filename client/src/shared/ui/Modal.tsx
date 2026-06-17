import { Show } from "solid-js";
import { Transition } from "solid-transition-group";
import type { Component, JSX } from "solid-js";
import s from "./Modal.module.css";

export type ModalProps = {
  open: boolean;
  onClose: () => void;
  children: JSX.Element;
};

// Backdrop + centered panel. Clicking the backdrop (not the panel) closes.
// `<Show>` stays the source of truth (closed => no DOM); the Transition plays
// the enter animation (backdrop fade + panel scale/fade) when it mounts.
export const Modal: Component<ModalProps> = (props) => {
  return (
    <Show when={props.open}>
      <Transition name="modal" appear>
        <div data-testid="modal-backdrop" class={s.backdrop} onClick={() => props.onClose()}>
          <div class={s.panel} onClick={(e) => e.stopPropagation()}>
            {props.children}
          </div>
        </div>
      </Transition>
    </Show>
  );
};

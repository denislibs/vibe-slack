import { For, createSignal, type Component } from "solid-js";
import { Modal, Input, Avatar } from "../../shared/ui";
import s from "./NewDmModal.module.css";

export type Person = { user_id: string; username: string; email: string };

// Presentational modal for starting a DM. Parent owns open/close, search and the
// pick action; this widget reports the query as it changes and the chosen person.
export const NewDmModal: Component<{
  open: boolean;
  onClose: () => void;
  results: Person[];
  onQuery: (q: string) => void;
  onPick: (person: Person) => void;
  title?: string;
}> = (props) => {
  const [query, setQuery] = createSignal("");
  return (
    <Modal open={props.open} onClose={props.onClose}>
      <div class={s.body}>
        <h2 class={s.title}>{props.title ?? "New message"}</h2>
        <Input
          aria-label="Search people"
          placeholder="Search by name or email"
          value={query()}
          onInput={(e) => { setQuery(e.currentTarget.value); props.onQuery(e.currentTarget.value); }}
          autofocus
        />
        <div class={s.list}>
          <For each={props.results}>
            {(r) => (
              <div class={s.row} onClick={() => props.onPick(r)}>
                <Avatar name={r.username} size={28} />
                <div class={s.meta}>
                  <span class={s.name}>{r.username}</span>
                  <span class={s.email}>{r.email}</span>
                </div>
              </div>
            )}
          </For>
        </div>
      </div>
    </Modal>
  );
};

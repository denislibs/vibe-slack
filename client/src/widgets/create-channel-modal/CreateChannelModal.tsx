import { createSignal, type Component } from "solid-js";
import { Modal, Input, Button } from "../../shared/ui";
import s from "./CreateChannelModal.module.css";

// Presentational modal for creating a channel. Parent owns open/close and the
// create action; this widget only collects a name + visibility and reports them.
export const CreateChannelModal: Component<{
  open: boolean;
  onClose: () => void;
  onCreate: (name: string, visibility: "public" | "private") => void;
}> = (props) => {
  const [name, setName] = createSignal("");
  const [visibility, setVisibility] = createSignal<"public" | "private">("public");

  const submit = () => {
    const n = name().trim();
    if (!n) return;
    props.onCreate(n, visibility());
    setName("");
    setVisibility("public");
  };

  return (
    <Modal open={props.open} onClose={props.onClose}>
      <div class={s.body}>
        <h2 class={s.title}>Create a channel</h2>
        <Input
          aria-label="Channel name"
          placeholder="e.g. marketing"
          value={name()}
          onInput={(e) => setName(e.currentTarget.value)}
          autofocus
        />
        <div class={s.visibility}>
          <label class={s.choice}>
            <input
              type="radio"
              name="channel-visibility"
              value="public"
              checked={visibility() === "public"}
              onChange={() => setVisibility("public")}
            />
            Public
          </label>
          <label class={s.choice}>
            <input
              type="radio"
              name="channel-visibility"
              value="private"
              checked={visibility() === "private"}
              onChange={() => setVisibility("private")}
            />
            Private
          </label>
        </div>
        <div class={s.actions}>
          <Button variant="ghost" onClick={props.onClose}>
            Cancel
          </Button>
          <Button variant="primary" disabled={name().trim().length === 0} onClick={submit}>
            Create
          </Button>
        </div>
      </div>
    </Modal>
  );
};

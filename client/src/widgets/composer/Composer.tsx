import { createSignal, type Component } from "solid-js";
import { Button } from "../../shared/ui";
import s from "./Composer.module.css";

export const Composer: Component<{ onSend: (text: string) => void }> = (props) => {
  const [text, setText] = createSignal("");
  const submit = () => {
    const t = text().trim();
    if (!t) return;
    props.onSend(t);
    setText("");
  };
  return (
    <div class={s.composer}>
      <input
        class={s.input}
        type="text"
        value={text()}
        placeholder="Message"
        onInput={(e) => setText(e.currentTarget.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            submit();
          }
        }}
      />
      <Button variant="primary" onClick={submit}>Send</Button>
    </div>
  );
};

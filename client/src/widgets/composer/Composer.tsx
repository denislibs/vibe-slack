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
    <div class={s.outer}>
      <div class={s.box}>
        <div class={s.toolbar} aria-hidden="true">
          <span class={s.tool}>B</span>
          <span class={s.tool}><i>I</i></span>
          <span class={s.tool}>🔗</span>
        </div>
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
        <div class={s.strip}>
          <Button variant="primary" onClick={submit} disabled={!text().trim()}>Send</Button>
        </div>
      </div>
    </div>
  );
};

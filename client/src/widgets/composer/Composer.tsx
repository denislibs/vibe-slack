import { createSignal, type Component } from "solid-js";
import { Button } from "../../shared/ui";

export const Composer: Component<{ onSend: (text: string) => void }> = (props) => {
  const [text, setText] = createSignal("");
  const submit = () => {
    const t = text().trim();
    if (!t) return;
    props.onSend(t);
    setText("");
  };
  return (
    <div>
      <input type="text" value={text()} onInput={(e) => setText(e.currentTarget.value)} />
      <Button onClick={submit}>Send</Button>
    </div>
  );
};

import { type Component } from "solid-js";
import { createEditor, createEditorTransaction } from "solid-tiptap";
import StarterKit from "@tiptap/starter-kit";
import Placeholder from "@tiptap/extension-placeholder";
import { Button } from "../../shared/ui";
import s from "./Composer.module.css";

// Message composer backed by TipTap (ProseMirror via solid-tiptap). The toolbar
// toggles real marks (bold/italic/code) and the editor supports lists, links,
// etc. from StarterKit. NOTE: the app's message pipeline is plaintext (MLS
// encrypts/stores/renders strings), so we send `editor.getText()` — formatting
// is an editing affordance, not yet persisted. Full rich-text would need the
// message payload + MessageList rendering to carry HTML.
export const Composer: Component<{ onSend: (text: string) => void }> = (props) => {
  let ref!: HTMLDivElement;

  const submit = () => {
    const ed = editor();
    if (!ed) return;
    const text = ed.getText().trim();
    if (!text) return;
    props.onSend(text);
    ed.commands.clearContent();
    ed.commands.focus();
  };

  const editor = createEditor(() => ({
    element: ref,
    extensions: [StarterKit, Placeholder.configure({ placeholder: "Message" })],
    editorProps: {
      // Apply the input styling + an accessible name to the contenteditable.
      attributes: { class: s.input, "aria-label": "Message", role: "textbox" },
      // Enter sends; Shift+Enter inserts a newline (StarterKit hard-break).
      handleKeyDown: (_view, event) => {
        if (event.key === "Enter" && !event.shiftKey) {
          event.preventDefault();
          submit();
          return true;
        }
        return false;
      },
    },
  }));

  // Reactive toolbar state derived from the editor's transactions.
  const isEmpty = createEditorTransaction(editor, (ed) => (ed ? ed.isEmpty : true));
  const isBold = createEditorTransaction(editor, (ed) => (ed ? ed.isActive("bold") : false));
  const isItalic = createEditorTransaction(editor, (ed) => (ed ? ed.isActive("italic") : false));
  const isCode = createEditorTransaction(editor, (ed) => (ed ? ed.isActive("code") : false));

  return (
    <div class={s.outer}>
      <div class={s.box}>
        <div class={s.toolbar}>
          <button
            type="button"
            class={`${s.tool} ${s.bold}`}
            classList={{ [s.toolActive]: isBold() }}
            aria-label="Bold"
            aria-pressed={isBold()}
            onClick={() => editor()?.chain().focus().toggleBold().run()}
          >B</button>
          <button
            type="button"
            class={`${s.tool} ${s.italic}`}
            classList={{ [s.toolActive]: isItalic() }}
            aria-label="Italic"
            aria-pressed={isItalic()}
            onClick={() => editor()?.chain().focus().toggleItalic().run()}
          >I</button>
          <button
            type="button"
            class={`${s.tool} ${s.codeTool}`}
            classList={{ [s.toolActive]: isCode() }}
            aria-label="Code"
            aria-pressed={isCode()}
            onClick={() => editor()?.chain().focus().toggleCode().run()}
          >&lt;/&gt;</button>
        </div>
        <div class={s.editorWrap} ref={ref} />
        <div class={s.strip}>
          <Button variant="primary" onClick={submit} disabled={isEmpty()}>Send</Button>
        </div>
      </div>
    </div>
  );
};

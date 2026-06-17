import { createSignal, Show, type Component } from "solid-js";
import { Button, Input } from "../../shared/ui";
import s from "./LoginForm.module.css";

// Presentational auth form: collects credentials, delegates via callbacks. No logic.
export const LoginForm: Component<{
  onLogin: (email: string, password: string) => void;
  onRegister: (email: string, username: string, password: string) => void;
  error: string;
  busy: boolean;
}> = (props) => {
  const [email, setEmail] = createSignal("");
  const [username, setUsername] = createSignal("");
  const [password, setPassword] = createSignal("");
  return (
    <form class={s.form} onSubmit={(e) => e.preventDefault()}>
      <label class={s.field}>
        Email
        <Input aria-label="Email" type="email" value={email()} onInput={(e) => setEmail(e.currentTarget.value)} />
      </label>
      <label class={s.field}>
        Username
        <Input aria-label="Username" type="text" value={username()} onInput={(e) => setUsername(e.currentTarget.value)} />
      </label>
      <label class={s.field}>
        Password
        <Input aria-label="Password" type="password" value={password()} onInput={(e) => setPassword(e.currentTarget.value)} />
      </label>
      <Show when={props.error}>
        <p class={s.error} role="alert">{props.error}</p>
      </Show>
      <div class={s.actions}>
        <Button variant="primary" disabled={props.busy} onClick={() => props.onLogin(email(), password())}>Log in</Button>
        <Button variant="secondary" disabled={props.busy} onClick={() => props.onRegister(email(), username(), password())}>Register</Button>
      </div>
    </form>
  );
};

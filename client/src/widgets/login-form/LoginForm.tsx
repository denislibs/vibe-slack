import { createSignal, Show, type Component } from "solid-js";
import { Button } from "../../shared/ui";

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
    <form onSubmit={(e) => e.preventDefault()}>
      <label>Email<input aria-label="Email" type="email" value={email()} onInput={(e) => setEmail(e.currentTarget.value)} /></label>
      <label>Username<input aria-label="Username" type="text" value={username()} onInput={(e) => setUsername(e.currentTarget.value)} /></label>
      <label>Password<input aria-label="Password" type="password" value={password()} onInput={(e) => setPassword(e.currentTarget.value)} /></label>
      <Show when={props.error}><p role="alert">{props.error}</p></Show>
      <Button disabled={props.busy} onClick={() => props.onLogin(email(), password())}>Log in</Button>
      <Button disabled={props.busy} onClick={() => props.onRegister(email(), username(), password())}>Register</Button>
    </form>
  );
};

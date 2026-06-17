import { createSignal, Show, type Component } from "solid-js";
import { Button, Input } from "../../shared/ui";
import s from "./LoginForm.module.css";

// Presentational auth form with Sign in / Register tabs. Login needs only
// email + password; the username field appears only when registering. No logic.
export const LoginForm: Component<{
  onLogin: (email: string, password: string) => void;
  onRegister: (email: string, username: string, password: string) => void;
  error: string;
  busy: boolean;
}> = (props) => {
  const [mode, setMode] = createSignal<"login" | "register">("login");
  const [email, setEmail] = createSignal("");
  const [username, setUsername] = createSignal("");
  const [password, setPassword] = createSignal("");
  return (
    <div class={s.wrap}>
      <div class={s.tabs} role="tablist">
        <button type="button" class={mode() === "login" ? s.tabActive : s.tab} onClick={() => setMode("login")}>
          Sign in
        </button>
        <button type="button" class={mode() === "register" ? s.tabActive : s.tab} onClick={() => setMode("register")}>
          Register
        </button>
      </div>
      <form class={s.form} onSubmit={(e) => e.preventDefault()}>
        <label class={s.field}>
          Email
          <Input aria-label="Email" type="email" value={email()} onInput={(e) => setEmail(e.currentTarget.value)} />
        </label>
        <Show when={mode() === "register"}>
          <label class={s.field}>
            Username
            <Input aria-label="Username" type="text" value={username()} onInput={(e) => setUsername(e.currentTarget.value)} />
          </label>
        </Show>
        <label class={s.field}>
          Password
          <Input aria-label="Password" type="password" value={password()} onInput={(e) => setPassword(e.currentTarget.value)} />
        </label>
        <Show when={props.error}>
          <p class={s.error} role="alert">{props.error}</p>
        </Show>
        <div class={s.actions}>
          <Show
            when={mode() === "login"}
            fallback={
              <Button variant="primary" type="submit" disabled={props.busy} onClick={() => props.onRegister(email(), username(), password())}>
                Create account
              </Button>
            }
          >
            <Button variant="primary" type="submit" disabled={props.busy} onClick={() => props.onLogin(email(), password())}>
              Log in
            </Button>
          </Show>
        </div>
      </form>
    </div>
  );
};

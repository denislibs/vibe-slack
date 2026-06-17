import type { Component } from "solid-js";
import { LoginForm } from "../../widgets/login-form/LoginForm";
import s from "./AuthPage.module.css";

export const AuthPage: Component<{
  onLogin: (email: string, password: string) => void;
  onRegister: (email: string, username: string, password: string) => void;
  error: string;
  busy: boolean;
}> = (props) => (
  <div class={s.page}>
    <div class={s.card}>
      <h1 class={s.title}>Messenger</h1>
      <LoginForm onLogin={props.onLogin} onRegister={props.onRegister} error={props.error} busy={props.busy} />
    </div>
  </div>
);

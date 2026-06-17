import type { Component } from "solid-js";
import { LoginForm } from "../../widgets/login-form/LoginForm";

export const AuthPage: Component<{
  onLogin: (email: string, password: string) => void;
  onRegister: (email: string, username: string, password: string) => void;
  error: string;
  busy: boolean;
}> = (props) => (
  <div>
    <h1>Sign in</h1>
    <LoginForm onLogin={props.onLogin} onRegister={props.onRegister} error={props.error} busy={props.busy} />
  </div>
);

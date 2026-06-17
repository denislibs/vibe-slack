// @vitest-environment jsdom
import { render, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { AuthPage } from "./AuthPage";

describe("AuthPage", () => {
  it("submits email+password to onLogin", () => {
    const onLogin = vi.fn();
    const { getByLabelText, getByText } = render(() => (
      <AuthPage onLogin={onLogin} onRegister={vi.fn()} error="" busy={false} />
    ));
    fireEvent.input(getByLabelText("Email"), { target: { value: "a@corp" } });
    fireEvent.input(getByLabelText("Password"), { target: { value: "pw" } });
    fireEvent.click(getByText("Log in"));
    expect(onLogin).toHaveBeenCalledWith("a@corp", "pw");
  });

  it("shows an error message", () => {
    const { getByText } = render(() => (
      <AuthPage onLogin={vi.fn()} onRegister={vi.fn()} error="invalid email or password" busy={false} />
    ));
    expect(getByText("invalid email or password")).toBeTruthy();
  });
});

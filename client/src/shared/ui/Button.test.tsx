// @vitest-environment jsdom
import { render, fireEvent } from "@solidjs/testing-library";
import { describe, it, expect, vi } from "vitest";
import { Button } from "./Button";

describe("Button (ui-kit)", () => {
  it("renders label and fires onClick", () => {
    const onClick = vi.fn();
    const { getByRole } = render(() => <Button onClick={onClick}>Send</Button>);
    const btn = getByRole("button");
    expect(btn.textContent).toBe("Send");
    fireEvent.click(btn);
    expect(onClick).toHaveBeenCalledOnce();
  });

  it("is disabled when disabled prop set", () => {
    const { getByRole } = render(() => <Button disabled>X</Button>);
    expect((getByRole("button") as HTMLButtonElement).disabled).toBe(true);
  });
});
